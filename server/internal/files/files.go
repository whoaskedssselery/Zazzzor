// Package files keeps the images users upload (avatars, server icons) on
// disk. Every image is decoded, checked and re-encoded as a 256×256 PNG
// before it is stored, so what is served is always a clean PNG of known
// size, whatever was uploaded.
//
// Layout: <root>/<kind>/<owner_id>/<hash>.png, where hash is the first 16
// hex digits of the SHA-256 of the stored PNG. Paths are only ever built
// from a Kind constant, a snowflake ID and a validated hash.
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Kind is the kind of image, and the directory it lives in.
type Kind string

// Image kinds.
const (
	KindAvatar Kind = "avatars"
	KindIcon   Kind = "icons"
)

func (k Kind) valid() bool { return k == KindAvatar || k == KindIcon }

// Errors of SaveImage.
var (
	// ErrUnsupportedImage: not PNG, JPEG, WebP or GIF, unreadable, or larger
	// than MaxDimension / MaxPixels.
	ErrUnsupportedImage = errors.New("files: unsupported image")
	// ErrTooLarge: the input is over MaxImageBytes.
	ErrTooLarge = errors.New("files: image too large")
	// ErrBusy: too many images were being processed for too long.
	ErrBusy = errors.New("files: image processing is saturated")
)

// Store keeps images. Implementations must be safe for concurrent use.
type Store interface {
	// SaveImage reads an image of at most MaxImageBytes from r, normalizes
	// it and stores it for ownerID, returning its hash (16 lower-case hex
	// digits). The same image always gives the same hash.
	SaveImage(ctx context.Context, kind Kind, ownerID snowflake.ID, r io.Reader) (hash string, err error)
	// Delete removes an image; a missing one is not an error.
	Delete(kind Kind, ownerID snowflake.ID, hash string) error
}

var hashRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidHash reports whether s has the form of an image hash.
func ValidHash(s string) bool { return hashRE.MatchString(s) }

const (
	dirPerm  = 0o750
	filePerm = 0o640
)

// Disk is a Store on the local file system.
type Disk struct {
	root string
	sem  chan struct{} // bounds concurrent decodes (memory)
}

var _ Store = (*Disk)(nil)

// decodeWait is how long SaveImage waits for a decode slot before ErrBusy.
const decodeWait = 10 * time.Second

// maxConcurrentDecodes bounds how many images are decoded at once: a 40 MP
// image needs up to 160 MB while it is processed.
const maxConcurrentDecodes = 2

// NewDisk returns a Disk storing under root, creating root if needed.
func NewDisk(root string) (*Disk, error) {
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, fmt.Errorf("files: create %s: %w", root, err)
	}
	return &Disk{root: root, sem: make(chan struct{}, maxConcurrentDecodes)}, nil
}

// path builds the location of an image. Callers have validated kind and
// hash; ownerID is a number, so no component can hold a separator or "..".
func (d *Disk) path(kind Kind, ownerID snowflake.ID, hash string) string {
	return filepath.Join(d.dir(kind, ownerID), hash+".png")
}

func (d *Disk) dir(kind Kind, ownerID snowflake.ID) string {
	return filepath.Join(d.root, string(kind), ownerID.String())
}

// SaveImage implements Store. The file is written to a temporary file in
// the target directory, synced and renamed into place, so readers never see
// a partial image.
func (d *Disk) SaveImage(ctx context.Context, kind Kind, ownerID snowflake.ID, r io.Reader) (string, error) {
	if !kind.valid() || ownerID <= 0 {
		return "", fmt.Errorf("files: invalid target %q/%d", kind, ownerID)
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("files: read image: %w", err)
	}
	if len(raw) > MaxImageBytes {
		return "", ErrTooLarge
	}

	if err = d.acquire(ctx); err != nil {
		return "", err
	}
	out, hash, err := normalize(raw)
	<-d.sem
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := d.write(kind, ownerID, hash, out); err != nil {
		return "", err
	}
	return hash, nil
}

func (d *Disk) write(kind Kind, ownerID snowflake.ID, hash string, data []byte) (err error) {
	dir := d.dir(kind, ownerID)
	if err = os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("files: create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".upload-*.tmp")
	if err != nil {
		return fmt.Errorf("files: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(filePerm); err != nil {
		return fmt.Errorf("files: chmod: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("files: write: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("files: sync: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("files: close: %w", err)
	}
	if err = os.Rename(tmp.Name(), d.path(kind, ownerID, hash)); err != nil {
		return fmt.Errorf("files: rename: %w", err)
	}
	syncDir(dir)
	return nil
}

// syncDir makes the rename durable where the platform allows it (Unix);
// failure only weakens durability after a crash, so it is ignored.
func syncDir(dir string) {
	f, err := os.Open(dir) //nolint:gosec // G304: dir is built from validated components.
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// Delete implements Store.
func (d *Disk) Delete(kind Kind, ownerID snowflake.ID, hash string) error {
	if !kind.valid() || ownerID <= 0 || !ValidHash(hash) {
		return fmt.Errorf("files: invalid image %q/%d/%q", kind, ownerID, hash)
	}
	err := os.Remove(d.path(kind, ownerID, hash))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("files: delete: %w", err)
	}
	return nil
}

// open returns the stored image, or an error wrapping os.ErrNotExist.
func (d *Disk) open(kind Kind, ownerID snowflake.ID, hash string) (*os.File, error) {
	return os.Open(d.path(kind, ownerID, hash)) //nolint:gosec // G304: validated components only.
}

// acquire takes a decode slot, waiting at most decodeWait.
func (d *Disk) acquire(ctx context.Context) error {
	t := time.NewTimer(decodeWait)
	defer t.Stop()
	select {
	case d.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ErrBusy
	}
}
