package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
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
	MaxDimension = 4096
	// MaxPixels is the largest accepted width × height: 16 Mi pixels, so
	// that a 4096×4096 square fits.
	MaxPixels = MaxDimension * MaxDimension
	// OutputSize is the side of every stored image.
	OutputSize = 256
)

// decodeBudget is the memory all concurrent decodes together may reserve
// (see imageInfo.weight). An image whose own estimate is larger is refused.
const decodeBudget = 384 << 20

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

// imageInfo is what is known about an upload before any pixel is decoded.
type imageInfo struct {
	format string // MIME type
	cfg    image.Config
	jpeg   jpegFacts // JPEG only
	// pngInterlaced is the Adam7 flag of a PNG.
	pngInterlaced bool
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUnsupportedImage}, args...)...)
}

// inspect detects the type from the content alone and reads the header:
// size limits (the decompression-bomb guard), and for JPEG the scan count
// and EXIF orientation. Nothing here allocates in proportion to the pixels.
func inspect(raw []byte) (imageInfo, error) {
	info := imageInfo{format: http.DetectContentType(raw)}
	c, ok := codecs[info.format]
	if !ok {
		return info, ErrUnsupportedImage
	}
	cfg, err := c.decodeConfig(bytes.NewReader(raw))
	if err != nil {
		return info, unsupported("%v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension ||
		int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return info, unsupported("%dx%d", cfg.Width, cfg.Height)
	}
	info.cfg = cfg
	switch info.format {
	case "image/jpeg":
		if info.jpeg, err = scanJPEG(raw); err != nil {
			return info, unsupported("%v", err)
		}
		if info.jpeg.scans > maxJPEGScans {
			return info, unsupported("more than %d scans", maxJPEGScans)
		}
	case "image/png":
		// IHDR: signature (8), length (4), type (4), then width, height,
		// depth, colour type, compression, filter, interlace (byte 28).
		info.pngInterlaced = len(raw) > 28 && raw[28] == 1
	}
	return info, nil
}

// weightOverhead covers the input copy, decoder tables, row buffers and the
// encoded output.
const weightOverhead = MaxImageBytes + 4<<20

// weight is a conservative estimate of the bytes decoding and resizing the
// image allocate, used to reserve memory before starting:
//
//   - JPEG: the decoded planes at up to 2 bytes per pixel per component (a
//     CMYK image is decoded to YCbCr plus black, then converted), plus 4
//     bytes per pixel per component of coefficients when progressive; sizes
//     padded to whole MCUs.
//   - PNG: bytes per pixel of the decoded type (8 for 16-bit RGBA), twice
//     for interlaced images (each Adam7 pass is decoded separately).
//   - GIF, WebP: 4 bytes per pixel, twice.
//
// Plus the Catmull-Rom scratch buffer (OutputSize × crop side × 32 bytes)
// and weightOverhead.
func (i imageInfo) weight() int64 {
	w, h := int64(i.cfg.Width), int64(i.cfg.Height)
	var decode int64
	switch i.format {
	case "image/jpeg":
		pw, ph := (w+31)&^31, (h+31)&^31
		comps := int64(max(i.jpeg.components, 1))
		decode = comps * pw * ph * 2
		if i.jpeg.progressive {
			decode += comps * pw * ph * 4
		}
	case "image/png":
		decode = w * h * pngBytesPerPixel(i.cfg.ColorModel)
		if i.pngInterlaced {
			decode *= 2
		}
	default:
		decode = w * h * 4 * 2
	}
	scratch := int64(OutputSize) * min(w, h) * 32
	return decode + scratch + int64(OutputSize*OutputSize*4) + weightOverhead
}

func pngBytesPerPixel(m color.Model) int64 {
	switch m {
	case color.GrayModel:
		return 1
	case color.Gray16Model:
		return 2
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	return 4 // RGBA, NRGBA, and anything unexpected
}

// render decodes the image, cuts a centred square, scales it to OutputSize,
// applies the EXIF orientation and encodes a PNG (dropping all metadata). It
// gives up between steps once ctx is done. It returns the PNG and its hash.
//
// Orientation is applied after crop and scale: rotating or mirroring maps
// the centred square of the stored image onto the centred square of the
// displayed one (up to one pixel of rounding), so the result is the same as
// orienting first, without a full-size copy.
func render(ctx context.Context, raw []byte, info imageInfo) (out []byte, hash string, err error) {
	src, err := codecs[info.format].decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", unsupported("%v", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
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
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if info.format == "image/jpeg" {
		dst = orient(dst, info.jpeg.orientation)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, "", fmt.Errorf("files: encode png: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])[:16], nil
}

// orient returns the square img as displayed for an EXIF orientation:
// 2 mirror, 3 rotate 180°, 4 flip, 5 transpose, 6 rotate 90° clockwise,
// 7 transverse, 8 rotate 90° counter-clockwise; 1 and anything else as is.
func orient(img *image.RGBA, orientation int) *image.RGBA {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	n := img.Bounds().Dx()
	out := image.NewRGBA(img.Bounds())
	m := n - 1
	for y := range n {
		for x := range n {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = m-x, y
			case 3:
				sx, sy = m-x, m-y
			case 4:
				sx, sy = x, m-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, m-x
			case 7:
				sx, sy = m-y, m-x
			case 8:
				sx, sy = m-y, x
			}
			si, di := img.PixOffset(sx, sy), out.PixOffset(x, y)
			copy(out.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return out
}
