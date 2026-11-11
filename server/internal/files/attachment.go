package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// MaxAttachmentBytes is the largest original file accepted for a message.
const MaxAttachmentBytes = 25 << 20

// ErrAttachmentTooLarge means an attachment exceeds MaxAttachmentBytes.
var ErrAttachmentTooLarge = errors.New("files: attachment too large")

// AttachmentInfo is the metadata read from the original bytes.
type AttachmentInfo struct {
	ContentType string
	Size        int64
	Width       *int32
	Height      *int32
	Digest      string
}

// AttachmentReference identifies an original file by channel and attachment ID.
func AttachmentReference(channelID, attachmentID snowflake.ID) Reference {
	return Reference{Kind: KindAttachment, OwnerID: channelID, Hash: attachmentID.String()}
}

// SaveAttachment streams and hashes the original, then publishes it atomically.
func (d *Disk) SaveAttachment(ctx context.Context, ref Reference, src io.Reader) (AttachmentInfo, error) {
	if ref.Kind != KindAttachment || !ref.valid() {
		return AttachmentInfo{}, fmt.Errorf("files: invalid attachment reference %v", ref)
	}
	digest := sha256.New()
	var info AttachmentInfo
	writeErr := d.writeFile(d.path(ref.Kind, ref.OwnerID, ref.Hash), d.root, func(f *os.File) error {
		n, err := io.Copy(io.MultiWriter(f, digest), io.LimitReader(src, MaxAttachmentBytes+1))
		if err != nil {
			return fmt.Errorf("files: copy attachment: %w", err)
		}
		if n > MaxAttachmentBytes {
			return ErrAttachmentTooLarge
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("files: save attachment: %w", ctxErr)
		}
		info, err = inspectAttachment(f, n)
		return err
	})
	if writeErr != nil {
		return AttachmentInfo{}, writeErr
	}
	info.Digest = hex.EncodeToString(digest.Sum(nil))
	return info, nil
}

// inspectAttachment reads the signature and, for images, their header without decoding pixels.
func inspectAttachment(f *os.File, size int64) (AttachmentInfo, error) {
	info := AttachmentInfo{Size: size, ContentType: "application/octet-stream"}
	var head [512]byte
	n, err := f.ReadAt(head[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return info, fmt.Errorf("files: read attachment header: %w", err)
	}
	mime := http.DetectContentType(head[:n])
	if decoder, ok := codecs[mime]; ok {
		cfg, err := decoder.decodeConfig(io.NewSectionReader(f, 0, size))
		if err == nil && cfg.Width > 0 && cfg.Width <= 1<<31-1 && cfg.Height > 0 && cfg.Height <= 1<<31-1 {
			width, height := int32(cfg.Width), int32(cfg.Height) //nolint:gosec // G115: checked dimensions.
			info.ContentType, info.Width, info.Height = mime, &width, &height
		}
		return info, nil
	}
	switch mime {
	case "video/mp4", "video/webm", "audio/mpeg", "audio/wave":
		info.ContentType = mime
	case "application/ogg":
		info.ContentType = "audio/ogg"
	default:
		// MP3 files without an ID3 tag start directly with an MPEG Layer III frame.
		if n >= 4 && head[0] == 0xff && head[1]&0xe6 == 0xe2 &&
			head[1]&0x18 != 0x08 && head[2]&0xf0 != 0xf0 && head[2]&0x0c != 0x0c {
			info.ContentType = "audio/mpeg"
		}
	}
	return info, nil
}

// OpenAttachment opens only a live original file.
func (d *Disk) OpenAttachment(ref Reference) (*os.File, error) {
	if ref.Kind != KindAttachment || !ref.valid() {
		return nil, fmt.Errorf("files: invalid attachment reference %v", ref)
	}
	return d.open(ref.Kind, ref.OwnerID, ref.Hash)
}

// DeleteAttachment moves an original to the same retention trash as images.
func (d *Disk) DeleteAttachment(ref Reference) error {
	if ref.Kind != KindAttachment || !ref.valid() {
		return fmt.Errorf("files: invalid attachment reference %v", ref)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.trash(ref)
}

// writeFile writes to a staging directory, then syncs and renames within the data volume.
func (d *Disk) writeFile(target, tempDir string, write func(*os.File) error) error {
	tmp, err := os.CreateTemp(tempDir, ".upload-*.tmp")
	if err != nil {
		return fmt.Errorf("files: create temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := tmp.Chmod(filePerm); err != nil {
		return fmt.Errorf("files: chmod: %w", err)
	}
	if err := write(tmp); err != nil {
		return fmt.Errorf("files: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("files: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("files: close: %w", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("files: create directory: %w", err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("files: rename: %w", err)
	}
	syncDir(dir)
	return nil
}
