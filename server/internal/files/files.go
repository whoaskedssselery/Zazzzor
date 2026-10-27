// Package files keeps the images users upload (avatars, server icons) on
// disk. Every image is decoded, checked and re-encoded as a 256×256 PNG
// before it is stored, so what is served is always a clean PNG of known
// size, whatever was uploaded.
//
// Layout: <root>/<kind>/<owner_id>/<hash>.png, where hash is the first 16
// hex digits of the SHA-256 of the stored PNG. Paths are only ever built
// from a Kind constant, a snowflake ID and a validated hash.
//
// Delete does not remove an image: it moves it to
// <root>/trash/<kind>/<owner_id>/<hash>/<unix_nano>.png, named after the
// time of deletion, so a database restored from a backup taken before the
// delete still finds its images (see retention.go). Serving never reads
// trash.
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

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
	// Delete moves an image to trash, where it stays for the retention; a
	// missing one is not an error.
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
	root      string
	now       func() time.Time
	retention time.Duration
	// mu serializes the changes this process makes to the tree (writes,
	// moves to and from trash, purges), so an image is never moved while
	// it is being replaced. Other processes are not covered: see Reconcile.
	mu sync.Mutex
	// mem reserves the estimated decode memory of each image (budget
	// decodeBudget), so concurrent uploads cannot exhaust it together.
	mem *semaphore.Weighted
	// slots caps concurrent decodes at maxDecodes (GOMAXPROCS), so slow
	// generic resize paths cannot oversubscribe the CPU.
	slots      *semaphore.Weighted
	maxDecodes int
	// wait bounds how long a decode waits for a slot and memory together.
	wait time.Duration
}

var _ Store = (*Disk)(nil)

// decodeWait is how long SaveImage waits for decode memory before ErrBusy.
const decodeWait = 10 * time.Second

// Options configures a Disk. Zero fields take the defaults.
type Options struct {
	// Retention is how long a deleted image stays in trash before Purge
	// removes it for good; zero keeps trash forever.
	Retention time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// MaxDecodes caps concurrent image decodes; zero means
	// runtime.GOMAXPROCS(0).
	MaxDecodes int
}

// NewDisk returns a Disk storing under root, creating root and its image
// and trash directories if needed.
func NewDisk(root string, o Options) (*Disk, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MaxDecodes <= 0 {
		o.MaxDecodes = max(runtime.GOMAXPROCS(0), 1)
	}
	for _, dir := range []string{root, filepath.Join(root, string(KindAvatar)),
		filepath.Join(root, string(KindIcon)), filepath.Join(root, trashDir)} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, fmt.Errorf("files: create %s: %w", dir, err)
		}
	}
	return &Disk{
		root:       root,
		now:        o.Now,
		retention:  o.Retention,
		mem:        semaphore.NewWeighted(decodeBudget),
		slots:      semaphore.NewWeighted(int64(o.MaxDecodes)),
		maxDecodes: o.MaxDecodes,
		wait:       decodeWait,
	}, nil
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

	info, err := inspect(raw)
	if err != nil {
		return "", err
	}
	weight := info.weight()
	if weight > decodeBudget {
		return "", unsupported("needs %d MiB to decode", weight>>20)
	}
	if err = d.acquire(ctx, weight); err != nil {
		return "", err
	}
	out, hash, err := render(ctx, raw, info)
	d.release(weight)
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
	d.mu.Lock()
	defer d.mu.Unlock()
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
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.trash(Reference{Kind: kind, OwnerID: ownerID, Hash: hash})
}

// open returns the stored image, or an error wrapping os.ErrNotExist.
func (d *Disk) open(kind Kind, ownerID snowflake.ID, hash string) (*os.File, error) {
	return os.Open(d.path(kind, ownerID, hash)) //nolint:gosec // G304: validated components only.
}

// acquire takes a decode slot and reserves n bytes of decode memory,
// waiting at most d.wait for both together (then ErrBusy) or until ctx is
// done. On error nothing stays reserved.
func (d *Disk) acquire(ctx context.Context, n int64) error {
	waitCtx, cancel := context.WithTimeout(ctx, d.wait)
	defer cancel()
	busy := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrBusy
	}
	if err := d.slots.Acquire(waitCtx, 1); err != nil {
		return busy()
	}
	if err := d.mem.Acquire(waitCtx, n); err != nil {
		d.slots.Release(1)
		return busy()
	}
	return nil
}

// release gives back what acquire took.
func (d *Disk) release(n int64) {
	d.mem.Release(n)
	d.slots.Release(1)
}
