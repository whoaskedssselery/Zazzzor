package httpx

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"
)

// FieldError points at one invalid request field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problem is an RFC 7807 error response. It implements error so handlers can
// return it directly.
type Problem struct {
	Status int
	Code   string
	Title  string
	Detail string
	Errors []FieldError

	retryAfter time.Duration
}

func (p *Problem) Error() string { return p.Code }

type problemJSON struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Code   string       `json:"code"`
	Errors []FieldError `json:"errors,omitempty"`
}

// WriteProblem writes p as application/problem+json.
func WriteProblem(w http.ResponseWriter, p *Problem) {
	if p == nil {
		p = Internal()
	}
	title := p.Title
	if title == "" {
		title = http.StatusText(p.Status)
	}
	body, err := json.Marshal(problemJSON{
		Type:   "urn:zazzzor:error:" + p.Code,
		Title:  title,
		Status: p.Status,
		Detail: p.Detail,
		Code:   p.Code,
		Errors: p.Errors,
	})
	if err != nil {
		// Only strings and ints inside; cannot happen.
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("X-Content-Type-Options", "nosniff")
	if p.retryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(int(math.Ceil(p.retryAfter.Seconds()))))
	}
	w.WriteHeader(p.Status)
	_, _ = w.Write(append(body, '\n'))
}

func newProblem(status int, code, detail string) *Problem {
	return &Problem{Status: status, Code: code, Title: http.StatusText(status), Detail: detail}
}

// BadRequest is 400 with a machine code, a human detail and optional field errors.
func BadRequest(code, detail string, errs ...FieldError) *Problem {
	p := newProblem(http.StatusBadRequest, code, detail)
	p.Errors = errs
	return p
}

// Unauthorized is 401.
func Unauthorized(code string) *Problem { return newProblem(http.StatusUnauthorized, code, "") }

// Forbidden is 403.
func Forbidden(code string) *Problem { return newProblem(http.StatusForbidden, code, "") }

// NotFound is 404 not_found.
func NotFound() *Problem { return newProblem(http.StatusNotFound, "not_found", "") }

// MethodNotAllowed is 405 method_not_allowed.
func MethodNotAllowed() *Problem {
	return newProblem(http.StatusMethodNotAllowed, "method_not_allowed", "")
}

// Conflict is 409.
func Conflict(code, detail string) *Problem { return newProblem(http.StatusConflict, code, detail) }

// Gone is 410.
func Gone(code string) *Problem { return newProblem(http.StatusGone, code, "") }

// UnsupportedMediaType is 415 unsupported_media_type.
func UnsupportedMediaType(detail string) *Problem {
	return newProblem(http.StatusUnsupportedMediaType, "unsupported_media_type", detail)
}

// TooManyRequests is 429 rate_limited with a Retry-After header in whole
// seconds, rounded up.
func TooManyRequests(retryAfter time.Duration) *Problem {
	p := newProblem(http.StatusTooManyRequests, "rate_limited", "")
	p.retryAfter = retryAfter
	return p
}

// ServiceUnavailable is 503.
func ServiceUnavailable(code string) *Problem {
	return newProblem(http.StatusServiceUnavailable, code, "")
}

// ServiceUnavailableRetryAfter is 503 with a Retry-After header in whole
// seconds, rounded up.
func ServiceUnavailableRetryAfter(code string, retryAfter time.Duration) *Problem {
	p := newProblem(http.StatusServiceUnavailable, code, "")
	p.retryAfter = retryAfter
	return p
}

// Internal is 500 internal. It never carries details.
func Internal() *Problem { return newProblem(http.StatusInternalServerError, "internal", "") }
