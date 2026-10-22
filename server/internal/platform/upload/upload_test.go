package upload_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

type part struct {
	name    string
	content []byte
}

func form(t *testing.T, parts ...part) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, p := range parts {
		pw, err := w.CreateFormFile(p.name, "x.bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = pw.Write(p.content)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func read(t *testing.T, body *bytes.Buffer, contentType string, maxBytes int) ([]byte, error) {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/", body)
	r.Header.Set("Content-Type", contentType)
	return upload.ReadFile(httptest.NewRecorder(), r, "file", maxBytes)
}

func problemCode(t *testing.T, err error) (int, string) {
	t.Helper()
	p, ok := errors.AsType[*httpx.Problem](err)
	if !ok {
		t.Fatalf("error %v is not a problem", err)
	}
	return p.Status, p.Code
}

func TestReadFile(t *testing.T) {
	t.Parallel()
	const maxBytes = 1 << 10

	t.Run("returns the named part, skipping others", func(t *testing.T) {
		t.Parallel()
		body, ct := form(t, part{"other", bytes.Repeat([]byte("x"), 100)}, part{"file", []byte("hello")})
		data, err := read(t, body, ct, maxBytes)
		if err != nil || string(data) != "hello" {
			t.Fatalf("data = %q, err = %v", data, err)
		}
	})
	t.Run("exactly the limit is accepted", func(t *testing.T) {
		t.Parallel()
		body, ct := form(t, part{"file", bytes.Repeat([]byte("x"), maxBytes)})
		if data, err := read(t, body, ct, maxBytes); err != nil || len(data) != maxBytes {
			t.Fatalf("len = %d, err = %v", len(data), err)
		}
	})
	for name, tc := range map[string]struct {
		parts  []part
		ct     string
		status int
		code   string
	}{
		"one byte over":   {[]part{{"file", bytes.Repeat([]byte("x"), maxBytes+1)}}, "", http.StatusRequestEntityTooLarge, "file_too_large"},
		"huge other part": {[]part{{"other", bytes.Repeat([]byte("x"), 200<<10)}, {"file", []byte("x")}}, "", http.StatusRequestEntityTooLarge, "file_too_large"},
		"missing part":    {[]part{{"other", []byte("x")}}, "", http.StatusBadRequest, "validation_failed"},
		"not multipart":   {nil, "application/json", http.StatusUnsupportedMediaType, "unsupported_media_type"},
		"no boundary":     {nil, "multipart/form-data", http.StatusUnsupportedMediaType, "unsupported_media_type"},
		"malformed body":  {nil, "multipart/form-data; boundary=zzz", http.StatusBadRequest, "invalid_body"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body, ct := form(t, tc.parts...)
			if tc.ct != "" {
				ct = tc.ct
				// A part whose header line has no colon.
				body = bytes.NewBufferString(strings.Join([]string{"--zzz", "not a header", "", "x", "--zzz--", ""}, crlf))
			}
			_, err := read(t, body, ct, maxBytes)
			if status, code := problemCode(t, err); status != tc.status || code != tc.code {
				t.Fatalf("got %d %s, want %d %s", status, code, tc.status, tc.code)
			}
		})
	}
}

func TestTooLargeDetail(t *testing.T) {
	t.Parallel()
	if d := upload.TooLarge(4 << 20).Detail; !strings.Contains(d, "4 MiB") {
		t.Fatalf("detail = %q", d)
	}
}

// crlf is the multipart line break.
var crlf = string([]byte{13, 10})
