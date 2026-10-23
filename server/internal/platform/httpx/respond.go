// Package httpx holds the HTTP plumbing shared by every module: JSON and
// problem+json responses, request decoding, the Origin guard and client IPs.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// JSON writes v as an application/json response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// A value that cannot be marshalled is a programming error; never
		// send a half-written body.
		slog.Error("httpx: marshal response", "err", err)
		WriteProblem(w, Internal())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// busyRetryAfter is the Retry-After of 503 server_busy from WriteError.
const busyRetryAfter = 2 * time.Second

// Handler is an http.HandlerFunc that may fail. See Wrap.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Wrap adapts h to an http.HandlerFunc; a returned error is written by
// WriteError.
func Wrap(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

// WriteError answers a failed request. A *Problem (also when wrapped) is
// written as is. A deadline that ran out while the request itself is still
// alive means a bounded wait inside the server gave up (the database
// pool's acquire timeout): the server is overloaded, not broken, so it is
// 503 server_busy with a Retry-After. Any other error becomes 500 internal
// and is logged, never echoed to the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	if p, ok := errors.AsType[*Problem](err); ok {
		WriteProblem(w, p)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		LoggerFrom(ctx).WarnContext(ctx, "request timed out waiting for a resource",
			"method", r.Method, "err", err.Error())
		WriteProblem(w, ServiceUnavailableRetryAfter("server_busy", busyRetryAfter))
		return
	}
	LoggerFrom(ctx).ErrorContext(ctx, "request failed", "method", r.Method, "err", err.Error())
	WriteProblem(w, Internal())
}

type loggerKey struct{}

// WithLogger returns ctx carrying a request-scoped logger.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// LoggerFrom returns the logger stored by WithLogger, or slog.Default().
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
