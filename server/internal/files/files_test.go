package files_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

const owner snowflake.ID = 1234567890

var hashRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

func newDisk(t *testing.T) (*files.Disk, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := files.NewDisk(dir, files.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return d, dir
}

// gradient is a w×h image whose left half is red and right half blue, so a
// centre crop is visible in the output.
func gradient(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeader is a PNG signature and IHDR chunk claiming w×h pixels and
// nothing else: enough for DecodeConfig, a bomb for a full decode.
func pngHeader(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit RGBA
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(13)) // IHDR data length
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

// tinyWebP is a valid 1×1 lossless WebP (Go has no WebP encoder).
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // G304: a path under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("stored file is not a PNG: %v", err)
	}
	return img
}

func TestSaveImageNormalizes(t *testing.T) {
	t.Parallel()
	d, dir := newDisk(t)
	src := encodeJPEG(t, gradient(800, 600))

	hash, err := d.SaveImage(t.Context(), files.KindAvatar, owner, bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if !hashRE.MatchString(hash) {
		t.Fatalf("hash = %q, want 16 hex digits", hash)
	}
	path := filepath.Join(dir, "avatars", owner.String(), hash+".png")
	img := readPNG(t, path)
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("stored size = %v, want 256x256", b)
	}
	// Centre crop of 800×600 is x 100..700: still red on the left, blue on
	// the right.
	if r, _, b, _ := img.At(10, 128).RGBA(); r < 0xE000 || b > 0x2000 {
		t.Fatalf("left pixel = %v, want red", img.At(10, 128))
	}
	if r, _, b, _ := img.At(245, 128).RGBA(); b < 0xE000 || r > 0x2000 {
		t.Fatalf("right pixel = %v, want blue", img.At(245, 128))
	}

	again, err := d.SaveImage(t.Context(), files.KindAvatar, owner, bytes.NewReader(src))
	if err != nil || again != hash {
		t.Fatalf("same image again = %q, %v; want %q", again, err, hash)
	}

	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{filepath.Dir(path): 0o750, path: 0o640} {
			st, statErr := os.Stat(p)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if got := st.Mode().Perm(); got != want {
				t.Errorf("%s mode = %v, want %v", p, got, want)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory holds %v (%v), want only the image (no temp files)", entries, err)
	}
}

func TestSaveImageAcceptsFormats(t *testing.T) {
	t.Parallel()
	d, _ := newDisk(t)
	webp, err := base64.StdEncoding.DecodeString(tinyWebP)
	if err != nil {
		t.Fatal(err)
	}
	var anim bytes.Buffer
	pal := color.Palette{color.Black, color.White}
	frame := func(c uint8) *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 40, 30), pal)
		for i := range p.Pix {
			p.Pix[i] = c
		}
		return p
	}
	if err := gif.EncodeAll(&anim, &gif.GIF{Image: []*image.Paletted{frame(1), frame(0)}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string][]byte{
		"png":  encodePNG(t, gradient(300, 500)),
		"jpeg": encodeJPEG(t, gradient(64, 64)),
		"gif":  anim.Bytes(),
		"webp": webp,
	} {
		if _, err := d.SaveImage(t.Context(), files.KindIcon, owner, bytes.NewReader(src)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSaveImageRejects(t *testing.T) {
	t.Parallel()
	d, dir := newDisk(t)
	full := encodePNG(t, gradient(50, 50))
	for name, src := range map[string][]byte{
		"text":            []byte("hello, I am not an image at all\n"),
		"empty":           nil,
		"truncated png":   full[:len(full)/2],
		"svg":             []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"too wide":        pngHeader(8193, 10),
		"too tall":        pngHeader(10, 8193),
		"too many pixels": pngHeader(8000, 6000), // 48 MP
		"zero size":       pngHeader(0, 0),
	} {
		_, err := d.SaveImage(t.Context(), files.KindAvatar, owner, bytes.NewReader(src))
		if !errors.Is(err, files.ErrUnsupportedImage) {
			t.Errorf("%s: err = %v, want ErrUnsupportedImage", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "avatars")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "avatars", owner.String()))
		if len(entries) != 0 {
			t.Fatalf("rejected images left files: %v", entries)
		}
	}
}

func TestSaveImageTooLarge(t *testing.T) {
	t.Parallel()
	d, _ := newDisk(t)
	src := io.MultiReader(bytes.NewReader(encodePNG(t, gradient(10, 10))), bytes.NewReader(make([]byte, files.MaxImageBytes)))
	if _, err := d.SaveImage(t.Context(), files.KindAvatar, owner, src); !errors.Is(err, files.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	d, dir := newDisk(t)
	hash, err := d.SaveImage(t.Context(), files.KindAvatar, owner, bytes.NewReader(encodePNG(t, gradient(20, 20))))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(files.KindAvatar, owner, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "avatars", owner.String(), hash+".png")); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}
	// Deleting what is not there is fine; a malformed hash is refused.
	if err := d.Delete(files.KindAvatar, owner, hash); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if err := d.Delete(files.KindAvatar, owner, "../../etc/passwd"); err == nil {
		t.Fatal("Delete accepted a malformed hash")
	}
}

func router(d *files.Disk) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { files.Mount(r, d) })
	return r
}

func TestServeFile(t *testing.T) {
	t.Parallel()
	d, _ := newDisk(t)
	hash, err := d.SaveImage(t.Context(), files.KindAvatar, owner, bytes.NewReader(encodePNG(t, gradient(20, 20))))
	if err != nil {
		t.Fatal(err)
	}
	c := testkit.NewClient(t, router(d))
	resp := c.Do(http.MethodGet, "/api/v1/files/avatars/"+owner.String()+"/"+hash+".png", nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.Status, resp.Body)
	}
	for k, want := range map[string]string{
		"Content-Type":            "image/png",
		"Cache-Control":           "public, max-age=31536000, immutable",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if _, err := png.Decode(bytes.NewReader(resp.Body)); err != nil {
		t.Fatalf("body is not a PNG: %v", err)
	}

	for _, path := range []string{
		"/api/v1/files/avatars/" + owner.String() + "/0000000000000000.png", // absent
		"/api/v1/files/icons/" + owner.String() + "/" + hash + ".png",       // other kind
		"/api/v1/files/avatars/1/" + hash + ".png",                          // other owner
	} {
		if resp := c.Do(http.MethodGet, path, nil); resp.Status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.Status)
		}
	}
}

func TestFilesRejectTraversal(t *testing.T) {
	t.Parallel()
	d, dir := newDisk(t)
	// A file outside the store that a traversal would reach.
	secret := filepath.Join(dir, "etc.png")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "avatars", "1"), 0o750); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router(d))
	t.Cleanup(srv.Close)

	for _, path := range []string{
		"/api/v1/files/avatars/1/..%2F..%2Fetc.png",
		"/api/v1/files/avatars/1/..%2f..%2fetc.png",
		"/api/v1/files/avatars/1/../../etc.png",
		"/api/v1/files/avatars/..%2F..%2F/0123456789abcdef.png",
		"/api/v1/files/avatars/1/0123456789ABCDEF.png",
		"/api/v1/files/avatars/1/0123456789abcdef.PNG",
		"/api/v1/files/avatars/1/0123456789abcde.png",
		"/api/v1/files/avatars/1/0123456789abcdef0.png",
		"/api/v1/files/avatars/-1/0123456789abcdef.png",
		"/api/v1/files/avatars/01/0123456789abcdef.png",
		"/api/v1/files/avatars/1/0123456789abcdef.png%00",
		"/api/v1/files/..%2Favatars/1/0123456789abcdef.png",
		"/api/v1/files/other/1/0123456789abcdef.png",
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
		if strings.Contains(string(body), "secret") {
			t.Errorf("GET %s leaked the file outside the store", path)
		}
	}
}
