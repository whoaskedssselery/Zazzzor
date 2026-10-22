package files

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// segment is a JPEG marker segment with its length field.
func segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2)) //nolint:gosec // G115: small test payloads
	return append(out, payload...)
}

// progressiveJPEG builds a valid progressive JPEG of w×h with nc components
// (1, 3 or 4; 4 carries an Adobe CMYK marker) holding only a DC scan of
// zero coefficients: tiny on disk, full size once decoded. Go has no
// progressive encoder, so the file is assembled by hand. scans repeats the
// SOS segment (only the first one is a meaningful scan).
func progressiveJPEG(w, h, nc, scans int) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	if nc == 4 {
		b.Write(segment(0xEE, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 0})) // transform 0: CMYK
	}
	dqt := make([]byte, 65)
	for i := 1; i < 65; i++ {
		dqt[i] = 1
	}
	b.Write(segment(0xDB, dqt))
	sof := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(nc)} //nolint:gosec // G115: test sizes fit
	for i := range nc {
		sof = append(sof, byte(i+1), 0x11, 0)
	}
	b.Write(segment(0xC2, sof))
	// DC table 0: a single code "0" for category 0 (difference 0).
	dht := append([]byte{0x00, 1}, make([]byte, 15)...)
	b.Write(segment(0xC4, append(dht, 0)))

	blocks := ((w + 7) / 8) * ((h + 7) / 8) * nc
	data := make([]byte, (blocks+7)/8) // one 0 bit per block
	sos := []byte{byte(nc)}            //nolint:gosec // G115: 1 to 4 components
	for i := range nc {
		sos = append(sos, byte(i+1), 0x00)
	}
	sos = append(sos, 0, 0, 0)
	for range scans {
		b.Write(segment(0xDA, sos))
		b.Write(data)
	}
	b.Write([]byte{0xFF, 0xD9})
	return b.Bytes()
}

// withExif inserts an APP1 Exif segment with the given orientation right
// after SOI.
func withExif(jpg []byte, orientation uint16, bigEndian bool) []byte {
	var order binary.AppendByteOrder = binary.LittleEndian
	tiff := []byte{'I', 'I'}
	if bigEndian {
		order, tiff = binary.BigEndian, []byte{'M', 'M'}
	}
	tiff = order.AppendUint16(tiff, 42)
	tiff = order.AppendUint32(tiff, 8)      // IFD0 right after the header
	tiff = order.AppendUint16(tiff, 2)      // two entries
	tiff = order.AppendUint16(tiff, 0x010F) // Make, ASCII, 4 bytes inline
	tiff = order.AppendUint16(tiff, 2)
	tiff = order.AppendUint32(tiff, 4)
	tiff = append(tiff, 'Z', 'Z', 'Z', 0)
	tiff = order.AppendUint16(tiff, 0x0112) // Orientation, SHORT, 1
	tiff = order.AppendUint16(tiff, 3)
	tiff = order.AppendUint32(tiff, 1)
	tiff = order.AppendUint16(tiff, orientation)
	tiff = order.AppendUint16(tiff, 0)
	tiff = order.AppendUint32(tiff, 0) // no next IFD
	app1 := segment(0xE1, append([]byte("Exif\x00\x00"), tiff...))
	out := append([]byte{0xFF, 0xD8}, app1...)
	return append(out, jpg[2:]...)
}

func newTestDisk(t *testing.T) *Disk {
	t.Helper()
	d, err := NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func storedImage(t *testing.T, d *Disk, kind Kind, hash string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join(d.dir(kind, 1), hash+".png")) //nolint:gosec // G304: test dir
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestProgressiveFixtureDecodes(t *testing.T) {
	img, err := jpeg.Decode(bytes.NewReader(progressiveJPEG(64, 48, 3, 1)))
	if err != nil {
		t.Fatalf("fixture does not decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("fixture size %v", b)
	}
}

func TestSaveImageDimensionCaps(t *testing.T) {
	d := newTestDisk(t)
	for name, src := range map[string][]byte{
		"4097 wide":  progressiveJPEG(4097, 8, 1, 1),
		"4097 tall":  progressiveJPEG(8, 4097, 1, 1),
		"png 4097":   pngIHDR(4097, 1, 8, 6, 0),
		"over 16 MP": pngIHDR(4096, 4097, 8, 6, 0),
	} {
		if _, err := d.SaveImage(t.Context(), KindAvatar, 1, bytes.NewReader(src)); !errors.Is(err, ErrUnsupportedImage) {
			t.Errorf("%s: err = %v, want ErrUnsupportedImage", name, err)
		}
	}
}

func TestSaveImageRejectsManyScans(t *testing.T) {
	d := newTestDisk(t)
	if _, err := d.SaveImage(t.Context(), KindAvatar, 1, bytes.NewReader(progressiveJPEG(64, 64, 3, 101))); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("101 scans: err = %v, want ErrUnsupportedImage", err)
	}
}

// measureAlloc reports the bytes allocated while f runs (an upper bound of
// its peak use).
func measureAlloc(f func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// The largest images within the caps go through, and the memory they take
// stays below what was reserved for them.
func TestWorstCaseImagesFitBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a few hundred MB")
	}
	d := newTestDisk(t)
	for name, src := range map[string][]byte{
		"progressive YCbCr 4096x4096":     progressiveJPEG(4096, 4096, 3, 1),
		"16-bit interlaced png 4096x4096": interlaced16PNG(t, 4096, 4096),
	} {
		info, err := inspect(src)
		if err != nil {
			t.Fatalf("%s: inspect: %v", name, err)
		}
		w := info.weight()
		if w > decodeBudget {
			t.Fatalf("%s: weight %d over the budget %d", name, w, decodeBudget)
		}
		var saveErr error
		used := measureAlloc(func() {
			_, saveErr = d.SaveImage(t.Context(), KindAvatar, 1, bytes.NewReader(src))
		})
		if saveErr != nil {
			t.Fatalf("%s: %v", name, saveErr)
		}
		if used > uint64(w) { //nolint:gosec // G115: weights are positive
			t.Errorf("%s: allocated %d bytes, reserved only %d", name, used, w)
		}
		t.Logf("%s: weight %d MiB, allocated %d MiB", name, w>>20, used>>20)
	}
	// A progressive CMYK image of the maximum size cannot fit the budget.
	if _, err := d.SaveImage(t.Context(), KindAvatar, 1, bytes.NewReader(progressiveJPEG(4096, 4096, 4, 1))); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("CMYK 4096x4096 progressive: err = %v, want ErrUnsupportedImage", err)
	}
}

// quadrants is a w×h image: top-left red, top-right green, bottom-left
// blue, bottom-right white.
func quadrants(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			var c color.RGBA
			switch {
			case x < w/2 && y < h/2:
				c = color.RGBA{R: 255, A: 255}
			case y < h/2:
				c = color.RGBA{G: 255, A: 255}
			case x < w/2:
				c = color.RGBA{B: 255, A: 255}
			default:
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func colorName(c color.Color) string {
	r, g, b, _ := c.RGBA()
	hi := func(v uint32) bool { return v > 0xA000 }
	switch {
	case hi(r) && hi(g) && hi(b):
		return "white"
	case hi(r) && !hi(g) && !hi(b):
		return "red"
	case !hi(r) && hi(g) && !hi(b):
		return "green"
	case !hi(r) && !hi(g) && hi(b):
		return "blue"
	}
	return "other"
}

func TestSaveImageHonoursExifOrientation(t *testing.T) {
	d := newTestDisk(t)
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, quadrants(96, 64), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	// Displayed quadrants TL, TR, BL, BR for each orientation of the
	// stored TL red, TR green, BL blue, BR white.
	for _, tc := range []struct {
		orientation uint16
		want        [4]string
	}{
		{1, [4]string{"red", "green", "blue", "white"}},
		{2, [4]string{"green", "red", "white", "blue"}}, // mirrored
		{3, [4]string{"white", "blue", "green", "red"}}, // 180°
		{6, [4]string{"blue", "red", "white", "green"}}, // 90° clockwise
		{8, [4]string{"green", "white", "red", "blue"}}, // 90° counter-clockwise
	} {
		for _, big := range []bool{false, true} {
			src := withExif(plain.Bytes(), tc.orientation, big)
			hash, err := d.SaveImage(t.Context(), KindAvatar, 1, bytes.NewReader(src))
			if err != nil {
				t.Fatalf("orientation %d: %v", tc.orientation, err)
			}
			img := storedImage(t, d, KindAvatar, hash)
			got := [4]string{
				colorName(img.At(64, 64)), colorName(img.At(192, 64)),
				colorName(img.At(64, 192)), colorName(img.At(192, 192)),
			}
			if got != tc.want {
				t.Errorf("orientation %d (big endian %v): quadrants %v, want %v", tc.orientation, big, got, tc.want)
			}
		}
	}
}

// pngIHDR is a PNG signature and IHDR chunk only (enough for DecodeConfig).
func pngIHDR(w, h uint32, depth, colorType, interlace byte) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{depth, colorType, 0, 0, interlace})
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(13))
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

func pngChunk(typ string, data []byte) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(len(data))) //nolint:gosec // G115: test data
	body := append([]byte(typ), data...)
	out.Write(body)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(body))
	return out.Bytes()
}

// interlaced16PNG is a w×h Adam7-interlaced 16-bit RGBA PNG (Go's encoder
// writes neither interlacing nor, for this input, the worst pixel format).
func interlaced16PNG(t *testing.T, w, h int) []byte {
	t.Helper()
	passes := [7][4]int{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}
	var raw bytes.Buffer
	zw := zlib.NewWriter(&raw)
	for _, p := range passes {
		pw := (w - p[0] + p[2] - 1) / p[2]
		ph := (h - p[1] + p[3] - 1) / p[3]
		if pw <= 0 || ph <= 0 {
			continue
		}
		row := make([]byte, 1+pw*8) // filter 0, transparent black
		for range ph {
			if _, err := zw.Write(row); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	ihdr := pngIHDR(uint32(w), uint32(h), 16, 6, 1) //nolint:gosec // G115: test sizes
	var out bytes.Buffer
	out.Write(ihdr)
	out.Write(pngChunk("IDAT", raw.Bytes()))
	out.Write(pngChunk("IEND", nil))
	return out.Bytes()
}

func TestWeightEstimate(t *testing.T) {
	const mib = 1 << 20
	base := int64(OutputSize*OutputSize*4 + weightOverhead)
	for _, tc := range []struct {
		name string
		info imageInfo
		want int64
	}{
		{"baseline YCbCr 1024x768", imageInfo{format: "image/jpeg", cfg: image.Config{Width: 1024, Height: 768},
			jpeg: jpegFacts{components: 3}}, 3*1024*768*2 + 256*768*32},
		{"progressive YCbCr 1000x1000 (MCU padding)", imageInfo{format: "image/jpeg", cfg: image.Config{Width: 1000, Height: 1000},
			jpeg: jpegFacts{components: 3, progressive: true}}, 3*1024*1024*6 + 256*1000*32},
		{"progressive CMYK 4096x4096", imageInfo{format: "image/jpeg", cfg: image.Config{Width: 4096, Height: 4096},
			jpeg: jpegFacts{components: 4, progressive: true}}, 4*4096*4096*6 + 256*4096*32},
		{"png RGBA 100x50", imageInfo{format: "image/png", cfg: image.Config{Width: 100, Height: 50, ColorModel: color.NRGBAModel}},
			100*50*4 + 256*50*32},
		{"png 16-bit interlaced 100x50", imageInfo{format: "image/png", pngInterlaced: true,
			cfg: image.Config{Width: 100, Height: 50, ColorModel: color.RGBA64Model}}, 100*50*8*2 + 256*50*32},
		{"png gray 100x50", imageInfo{format: "image/png", cfg: image.Config{Width: 100, Height: 50, ColorModel: color.GrayModel}},
			100*50 + 256*50*32},
		{"png paletted 100x50", imageInfo{format: "image/png", cfg: image.Config{Width: 100, Height: 50, ColorModel: color.Palette{color.Black}}},
			100*50 + 256*50*32},
		{"gif 100x50", imageInfo{format: "image/gif", cfg: image.Config{Width: 100, Height: 50}}, 100*50*8 + 256*50*32},
		{"webp 100x50", imageInfo{format: "image/webp", cfg: image.Config{Width: 100, Height: 50}}, 100*50*8 + 256*50*32},
	} {
		if got := tc.info.weight(); got != tc.want+base {
			t.Errorf("%s: weight = %d, want %d", tc.name, got, tc.want+base)
		}
	}
	if w := (imageInfo{format: "image/jpeg", cfg: image.Config{Width: 4096, Height: 4096},
		jpeg: jpegFacts{components: 4, progressive: true}}).weight(); w <= decodeBudget {
		t.Fatalf("progressive CMYK at the size cap (%d MiB) should exceed the budget", w/mib)
	}
}

func TestScanJPEG(t *testing.T) {
	for scans, want := range map[int]int{1: 1, 100: 100, 101: 101, 500: 101} {
		f, err := scanJPEG(progressiveJPEG(16, 16, 3, scans))
		if err != nil || f.scans != want || !f.progressive || f.components != 3 {
			t.Errorf("%d scans: %+v, %v; want %d counted", scans, f, err, want)
		}
	}
	var base bytes.Buffer
	if err := jpeg.Encode(&base, quadrants(16, 16), nil); err != nil {
		t.Fatal(err)
	}
	if f, err := scanJPEG(base.Bytes()); err != nil || f.progressive || f.scans != 1 || f.orientation != 1 {
		t.Errorf("baseline: %+v, %v", f, err)
	}
	for o := uint16(1); o <= 8; o++ {
		for _, big := range []bool{false, true} {
			if f, err := scanJPEG(withExif(base.Bytes(), o, big)); err != nil || f.orientation != int(o) {
				t.Errorf("orientation %d big %v: %+v, %v", o, big, f, err)
			}
		}
	}
	if f, _ := scanJPEG(withExif(base.Bytes(), 9, false)); f.orientation != 1 {
		t.Errorf("orientation 9 read as %d, want 1", f.orientation)
	}
	for name, b := range map[string][]byte{
		"empty":     nil,
		"no SOI":    []byte("hello"),
		"truncated": base.Bytes()[:20],
		"bad len":   {0xFF, 0xD8, 0xFF, 0xE0, 0xFF, 0xFF},
	} {
		if _, err := scanJPEG(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Malformed TIFF blocks never panic and mean "as stored".
	for _, tiff := range [][]byte{nil, []byte("II"), []byte("XX*\x00\x08\x00\x00\x00"),
		[]byte("II*\x00\xff\xff\xff\x7f"), []byte("II*\x00\x08\x00\x00\x00\xff\xff")} {
		if o := exifOrientation(tiff); o != 1 {
			t.Errorf("exifOrientation(%q) = %d", tiff, o)
		}
	}
}
