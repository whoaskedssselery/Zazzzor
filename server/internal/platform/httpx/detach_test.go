package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

type ctxKey struct{}

// seenCtx is what a handler saw of its context while it ran.
type seenCtx struct {
	err      error
	value    any
	deadline time.Time
	hasDL    bool
}

// served runs one request through Detach(timeout) with a context the client
// already cancelled.
func served(t *testing.T, method string, timeout time.Duration) seenCtx {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "v"))
	cancel() // the client went away
	var seen seenCtx
	h := httpx.Detach(timeout)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		c := r.Context()
		seen = seenCtx{err: c.Err(), value: c.Value(ctxKey{})}
		seen.deadline, seen.hasDL = c.Deadline()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, method, "/x", nil))
	return seen
}

func TestDetachKeepsWritesRunning(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		start := time.Now()
		seen := served(t, method, time.Minute)
		if seen.err != nil {
			t.Fatalf("%s: context cancelled with the client: %v", method, seen.err)
		}
		if seen.value != "v" {
			t.Fatalf("%s: request values lost", method)
		}
		if !seen.hasDL || seen.deadline.Before(start.Add(time.Minute)) || seen.deadline.After(time.Now().Add(time.Minute)) {
			t.Fatalf("%s: deadline = %v, %v", method, seen.deadline, seen.hasDL)
		}
	}
}

func TestDetachDefaultsTheTimeout(t *testing.T) {
	start := time.Now()
	if seen := served(t, http.MethodPost, 0); !seen.hasDL || seen.deadline.Before(start.Add(httpx.DefaultWriteDeadline)) {
		t.Fatalf("deadline = %v, %v", seen.deadline, seen.hasDL)
	}
}

func TestDetachLeavesReadsCancellable(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if served(t, method, time.Minute).err == nil {
			t.Fatalf("%s: a read outlived its client", method)
		}
	}
}
