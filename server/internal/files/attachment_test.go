package files_test

import (
	"bytes"
	"errors"
	"image"
	"io"
	"os"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
)

func TestAttachmentKeepsOriginalPNG(t *testing.T) {
	d, err := files.NewDisk(t.TempDir(), files.Options{})
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	raw := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 7, 9)))
	ref := files.AttachmentReference(1, 2)
	info, err := d.SaveAttachment(t.Context(), ref, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	if info.ContentType != "image/png" || info.Width == nil || *info.Width != 7 || info.Height == nil || *info.Height != 9 {
		t.Errorf("image metadata = %+v, want PNG 7x9", info)
	}
	f, err := d.OpenAttachment(ref)
	if err != nil {
		t.Fatalf("OpenAttachment: %v", err)
	}
	got, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil || !bytes.Equal(got, raw) {
		t.Errorf("read attachment = %d bytes, %v, want %d original bytes", len(got), err, len(raw))
	}
	if err := d.DeleteAttachment(ref); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	if f, err := d.OpenAttachment(ref); !errors.Is(err, os.ErrNotExist) {
		if f != nil {
			_ = f.Close()
		}
		t.Errorf("open trashed file = %v, want missing file", err)
	}
}
