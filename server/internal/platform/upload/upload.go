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
	"sync"
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
	// MaxInFlightPerUser is how many of them one user may run at once.
	MaxInFlightPerUser = 2
	// SlotBytes is the memory one upload slot accounts for.
	SlotBytes = 8 << 20
	// busyRetryAfter is the Retry-After of an upload turned away.
	busyRetryAfter = 5 * time.Second
)

// Gate bounds the upload requests in flight, in all and per user, so that
// one user's slow uploads cannot hold every slot.
type Gate struct {
	slots   chan struct{}
	perUser int

	mu       sync.Mutex
	inFlight map[string]int // by user key; absent means 0
}

// NewGate returns a Gate letting n uploads run at once, at most perUser of
// them for one user.
func NewGate(n, perUser int) *Gate {
	return &Gate{slots: make(chan struct{}, max(n, 1)), perUser: max(perUser, 1), inFlight: make(map[string]int)}
}

// Limit returns middleware running next while the request holds a slot of
// g and one of its user's, the user named by key (a request that passed
// authentication). Both are taken before any of the body is read. A user
// already at their cap is answered 429 rate_limited, without a free global
// slot the request is answered 503 server_busy; both carry a Retry-After.
func (g *Gate) Limit(key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := key(r)
			if !g.enter(user) {
				httpx.WriteProblem(w, httpx.TooManyRequests(busyRetryAfter))
				return
			}
			defer g.leave(user)
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
}

// enter counts an upload of user, unless they are at their cap.
func (g *Gate) enter(user string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight[user] >= g.perUser {
		return false
	}
	g.inFlight[user]++
	return true
}

func (g *Gate) leave(user string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight[user]--; g.inFlight[user] <= 0 {
		delete(g.inFlight, user)
	}
}

// bodies is the gate every upload route of the process shares.
var bodies = NewGate(MaxInFlight, MaxInFlightPerUser)

// Limit is the process-wide upload gate (MaxInFlight slots, at most
// MaxInFlightPerUser of them per user); mount it on every route that reads
// an upload, after authentication and rate limits, with the key naming the
// signed-in user (auth.UserKey).
func Limit(key func(*http.Request) string) func(http.Handler) http.Handler { return bodies.Limit(key) }
