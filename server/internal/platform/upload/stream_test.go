package upload_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

func TestStreamFileReadsBodyOnlyOnDemand(t *testing.T) {
	body, contentType := form(t, part{name: "file", content: make([]byte, 9<<20)})
	stream := &demandReader{Reader: body}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", stream)
	r.Header.Set("Content-Type", contentType)
	err := upload.StreamFile(httptest.NewRecorder(), r, 25<<20, func(_ string, r io.Reader) error {
		stream.inCallback = true
		defer func() { stream.inCallback = false }()
		n, err := io.Copy(io.Discard, r)
		if err == nil && n != 9<<20 {
			t.Errorf("copied %d bytes, want 9 MiB", n)
		}
		return err
	})
	if err != nil {
		t.Fatalf("StreamFile: %v", err)
	}
}

// demandReader allows the multipart header to be read before the file
// consumer starts. A buffered upload would read the body before the callback.
type demandReader struct {
	io.Reader
	n          int
	inCallback bool
}

func (r *demandReader) Read(p []byte) (int, error) {
	if !r.inCallback && r.n >= 4096 && r.n < 9<<20 {
		return 0, errors.New("test: file read before streaming consumer started")
	}
	n, err := r.Reader.Read(p)
	r.n += n
	return n, err
}

func TestStreamFileRejectsOversizedPartHeader(t *testing.T) {
	var raw bytes.Buffer
	m := multipart.NewWriter(&raw)
	_, err := m.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="file.bin"`},
		"X-Large":             {strings.Repeat("x", 128<<10)},
	})
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", &raw)
	r.Header.Set("Content-Type", m.FormDataContentType())
	err = upload.StreamFile(httptest.NewRecorder(), r, 25<<20, func(_ string, src io.Reader) error {
		t.Error("consumer ran after an oversized multipart header")
		_, err := io.Copy(io.Discard, src)
		return err
	})
	status, code := problemCode(t, err)
	if status != http.StatusBadRequest || code != "invalid_body" {
		t.Errorf("oversized header = %d %s, want 400 invalid_body", status, code)
	}
}
