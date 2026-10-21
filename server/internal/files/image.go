package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Limits on uploaded images.
const (
	// MaxImageBytes is the largest accepted upload (4 MiB).
	MaxImageBytes = 4 << 20
	// MaxDimension is the largest accepted width or height.
	MaxDimension = 8192
	// MaxPixels is the largest accepted width × height (40 megapixels).
	MaxPixels = 40_000_000
	// OutputSize is the side of every stored image.
	OutputSize = 256
)

type codec struct {
	decodeConfig func(io.Reader) (image.Config, error)
	decode       func(io.Reader) (image.Image, error)
}

// codecs by the MIME type http.DetectContentType reports. GIF decodes only
// the first frame.
var codecs = map[string]codec{
	"image/png":  {png.DecodeConfig, png.Decode},
	"image/jpeg": {jpeg.DecodeConfig, jpeg.Decode},
	"image/gif":  {gif.DecodeConfig, gif.Decode},
	"image/webp": {webp.DecodeConfig, webp.Decode},
}

// normalize turns an uploaded image into the stored PNG: the type comes from
// the content alone, the size is checked from the header before any pixel
// is decoded (decompression bombs), and the picture is cut to a centred
// square and scaled to OutputSize. Re-encoding drops all metadata. It
// returns the PNG and its hash.
func normalize(raw []byte) (out []byte, hash string, err error) {
	c, ok := codecs[http.DetectContentType(raw)]
	if !ok {
		return nil, "", ErrUnsupportedImage
	}
	cfg, err := c.decodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension ||
		int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, "", fmt.Errorf("%w: %dx%d", ErrUnsupportedImage, cfg.Width, cfg.Height)
	}
	src, err := c.decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}

	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	if side <= 0 {
		return nil, "", ErrUnsupportedImage
	}
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	crop := image.Rect(x0, y0, x0+side, y0+side)

	dst := image.NewRGBA(image.Rect(0, 0, OutputSize, OutputSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, "", fmt.Errorf("files: encode png: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])[:16], nil
}
