// Package httpx holds the HTTP plumbing shared by every module: JSON and
// problem+json responses, request decoding, the Origin guard and client IPs.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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

// Handler is an http.HandlerFunc that may fail. See Wrap.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Wrap adapts h to an http.HandlerFunc. A returned *Problem (also when
// wrapped) is written as is; any other error becomes 500 internal and is
// logged, never echoed to the client.
func Wrap(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}
		if p, ok := errors.AsType[*Problem](err); ok {
			WriteProblem(w, p)
			return
		}
		LoggerFrom(r.Context()).ErrorContext(r.Context(), "request failed",
			"method", r.Method, "err", err.Error())
		WriteProblem(w, Internal())
	}
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
