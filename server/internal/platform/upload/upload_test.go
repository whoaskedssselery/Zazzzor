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

// TestGateBoundsUploadsInFlight: past the gate's slots an upload is turned
// away 503 server_busy with a Retry-After before its body is read; a freed
// slot is usable again.
func TestGateBoundsUploadsInFlight(t *testing.T) {
	g := upload.NewGate(1, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	h := g.Limit(func(*http.Request) string { return "" })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	done := make(chan int)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/", nil))
		done <- rec.Code
	}()
	<-entered

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("second upload: status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"server_busy"`) || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second upload: %s, Retry-After %q", rec.Body.String(), rec.Header().Get("Retry-After"))
	}

	close(release)
	if code := <-done; code != http.StatusNoContent {
		t.Fatalf("first upload: status = %d", code)
	}
	go func() { <-entered }()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upload after release: status = %d, want 204", rec.Code)
	}
}

// TestUploadPerUserCap: one user may have at most perUser uploads in
// flight; the next is turned away 429 rate_limited with a Retry-After
// before its body is read and without taking a global slot, so other users
// still get in until the global slots run out.
func TestUploadPerUserCap(t *testing.T) {
	g := upload.NewGate(3, 2)
	entered := make(chan string)
	release := make(chan struct{})
	h := g.Limit(func(r *http.Request) string { return r.Header.Get("X-User") })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entered <- r.Header.Get("X-User")
			<-release
			w.WriteHeader(http.StatusNoContent)
		}))
	req := func(user string) *http.Request {
		r := httptest.NewRequest(http.MethodPut, "/", nil)
		r.Header.Set("X-User", user)
		return r
	}
	done := make(chan int, 3)
	hold := func(user string) {
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req(user))
			done <- rec.Code
		}()
		if got := <-entered; got != user {
			t.Fatalf("entered %q, want %q", got, user)
		}
	}
	hold("alice")
	hold("alice")
	// refused serves a request that must be turned away at once.
	refused := func(user string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		served := make(chan struct{})
		go func() { h.ServeHTTP(rec, req(user)); close(served) }()
		select {
		case <-served:
			return rec
		case who := <-entered:
			t.Fatalf("%s was let in", who)
		}
		return nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rec := refused("alice")
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"rate_limited"`) ||
		rec.Header().Get("Retry-After") == "" {
		t.Fatalf("alice's third upload: %d %s, Retry-After %q", rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
	}
	// The refusal took no global slot: bob gets the third one.
	hold("bob")
	rec = refused("carol")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("carol with every slot taken: status = %d, want 503", rec.Code)
	}

	close(release)
	for range 3 {
		if code := <-done; code != http.StatusNoContent {
			t.Fatalf("held upload: status = %d", code)
		}
	}
	go func() { <-entered }()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req("alice"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("alice after release: status = %d, want 204", rec.Code)
	}
}
