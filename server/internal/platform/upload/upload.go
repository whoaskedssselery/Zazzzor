// Package upload holds the HTTP plumbing of file uploads shared by the
// modules (avatars, server icons): longer deadlines than the global ones, a
// bounded streaming multipart reader and the 413 response.
package upload

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

const (
	// ReadTimeout replaces the global 15 s body deadline on upload routes:
	// 4 MiB over a 1 Mbit/s uplink takes about 32 s.
	ReadTimeout = 120 * time.Second
	// WriteTimeout leaves time to answer after a slow upload.
	WriteTimeout = ReadTimeout + 30*time.Second
	// multipartOverhead is what the form around the file may add.
	multipartOverhead = 64 << 10
)

// ExtendDeadlines gives an upload route more time than the global
// per-request deadlines. Writers without deadline support (tests) are fine.
func ExtendDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	now := time.Now()
	_ = rc.SetReadDeadline(now.Add(ReadTimeout))
	_ = rc.SetWriteDeadline(now.Add(WriteTimeout))
}

// TooLarge is 413 file_too_large for a file over maxBytes.
func TooLarge(maxBytes int) *httpx.Problem {
	return &httpx.Problem{Status: http.StatusRequestEntityTooLarge, Code: "file_too_large",
		Title: http.StatusText(http.StatusRequestEntityTooLarge), Detail: fmt.Sprintf("the file must be at most %s", size(maxBytes))}
}

func size(n int) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

// ReadFile returns the bytes of the part named field of a multipart body.
// The whole body may be at most maxBytes plus a small allowance for the
// form; the file itself may be at most maxBytes (413 file_too_large).
// Parts before field are skipped without being buffered. Errors are
// *httpx.Problem: 415 for a body that is not multipart/form-data, 400
// invalid_body for a malformed one, 400 validation_failed without the part.
func ReadFile(w http.ResponseWriter, r *http.Request, field string, maxBytes int) ([]byte, error) {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, httpx.UnsupportedMediaType("Content-Type must be multipart/form-data")
	}
	body := http.MaxBytesReader(w, r.Body, int64(maxBytes)+multipartOverhead)
	mr := multipart.NewReader(body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, httpx.BadRequest("validation_failed", "some fields are invalid",
				httpx.FieldError{Field: field, Message: "is required"})
		}
		if err != nil {
			return nil, readError(err, maxBytes)
		}
		if part.FormName() != field {
			continue // NextPart skips the rest of this part
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(maxBytes)+1))
		if err != nil {
			return nil, readError(err, maxBytes)
		}
		if len(data) > maxBytes {
			return nil, TooLarge(maxBytes)
		}
		return data, nil
	}
}

func readError(err error, maxBytes int) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return TooLarge(maxBytes)
	}
	return httpx.BadRequest("invalid_body", "request body is not valid multipart/form-data")
}

// Memory budget of the API process. deploy/docker-compose.yml gives the api
// container mem_limit 1g and GOMEMLIMIT 768MiB; the transient allocations
// that scale with load are each capped so that together they stay well
// below GOMEMLIMIT, leaving the rest for the heap of requests, gateway
// buffers and the runtime:
//
//	image decodes   files.decodeBudget          256 MiB
//	password hashes auth.maxConcurrentHashes    4 × 64 MiB = 256 MiB
//	upload bodies   MaxInFlight × SlotBytes     8 × 8 MiB  =  64 MiB
//	                                            total        576 MiB < 768 MiB
//
// An upload slot covers the body (at most 4 MiB, plus what io.ReadAll's
// growth and the copy SaveImage reads may add) for as long as the request
// runs, decode included; its decode memory is reserved separately.
const (
	// MaxInFlight is how many upload requests may run at once.
	MaxInFlight = 8
	// SlotBytes is the memory one upload slot accounts for.
	SlotBytes = 8 << 20
	// busyRetryAfter is the Retry-After of an upload turned away.
	busyRetryAfter = 5 * time.Second
)

// Gate bounds the upload requests in flight.
type Gate struct{ slots chan struct{} }

// NewGate returns a Gate letting n uploads run at once.
func NewGate(n int) *Gate { return &Gate{slots: make(chan struct{}, max(n, 1))} }

// Limit runs next while it holds a slot of g. Without a free slot the
// request is answered 503 server_busy with a Retry-After at once, before
// any of its body is read.
func (g *Gate) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case g.slots <- struct{}{}:
		default:
			httpx.WriteProblem(w, httpx.ServiceUnavailableRetryAfter("server_busy", busyRetryAfter))
			return
		}
		defer func() { <-g.slots }()
		next.ServeHTTP(w, r)
	})
}

// bodies is the gate every upload route of the process shares.
var bodies = NewGate(MaxInFlight)

// Limit is the process-wide upload gate (MaxInFlight slots); mount it on
// every route that reads an upload, after authentication and rate limits.
func Limit(next http.Handler) http.Handler { return bodies.Limit(next) }
