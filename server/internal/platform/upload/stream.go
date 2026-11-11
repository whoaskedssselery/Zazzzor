package upload

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// StreamFile consumes exactly one raw multipart file, without buffering its body.
// The callback must read through EOF or return an error.
func StreamFile(w http.ResponseWriter, r *http.Request, maxBytes int, save func(string, io.Reader) error) error {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return httpx.UnsupportedMediaType("Content-Type must be multipart/form-data")
	}
	body := http.MaxBytesReader(w, r.Body, int64(maxBytes)+multipartOverhead)
	defer func() { _ = body.Close() }()
	// A small upload can still carry a huge MIME header. Limit header
	// reads separately from the file and the total request.
	window := &io.LimitedReader{R: body, N: multipartOverhead}
	mr := multipart.NewReader(window, params["boundary"])
	part, err := mr.NextRawPart()
	if err != nil && window.N == 0 {
		return readError(err, maxBytes)
	}
	if errors.Is(err, io.EOF) || (err == nil && part.FormName() != "file") {
		return httpx.BadRequest("validation_failed", "some fields are invalid",
			httpx.FieldError{Field: "file", Message: "is required"})
	}
	if err != nil {
		return readError(err, maxBytes)
	}
	window.N = int64(maxBytes) + multipartOverhead
	if err := save(part.FileName(), filePart{part: part, maxBytes: maxBytes}); err != nil {
		return fmt.Errorf("upload: save multipart file: %w", err)
	}
	window.N = multipartOverhead
	if _, err := mr.NextRawPart(); !errors.Is(err, io.EOF) {
		return readError(err, maxBytes)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return readError(err, maxBytes)
	}
	return nil
}

// filePart separates malformed input from a failure writing to disk.
type filePart struct {
	part     *multipart.Part
	maxBytes int
}

func (p filePart) Read(b []byte) (int, error) {
	n, err := p.part.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, readError(err, p.maxBytes)
	}
	return n, err
}
