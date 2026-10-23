package httpx

import (
	"net/http"
	"time"
)

// Default per-request deadlines applied by Deadlines.
const (
	DefaultReadDeadline  = 15 * time.Second
	DefaultWriteDeadline = 30 * time.Second
)

// DeadlineOptions configures Deadlines. Zero fields take the defaults.
type DeadlineOptions struct {
	Read  time.Duration // reading the request body; default DefaultReadDeadline
	Write time.Duration // writing the response; default DefaultWriteDeadline
}

// Deadlines bounds how long each request may take to send its body and to
// receive its response, so a drip-fed body cannot hold a handler forever. The
// server deliberately has no ReadTimeout/WriteTimeout (they would cut
// long-lived WebSockets), so the deadlines are set per request here. Every
// request gets them, whatever its headers; a route that manages its own
// connection (the gateway) opts out with WithoutDeadlines.
func Deadlines(opts DeadlineOptions) func(http.Handler) http.Handler {
	if opts.Read <= 0 {
		opts.Read = DefaultReadDeadline
	}
	if opts.Write <= 0 {
		opts.Write = DefaultWriteDeadline
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := http.NewResponseController(w)
			// Errors only mean the writer cannot do deadlines (e.g. a test
			// recorder); there is nothing better to do than carry on.
			now := time.Now()
			_ = rc.SetReadDeadline(now.Add(opts.Read))
			_ = rc.SetWriteDeadline(now.Add(opts.Write))
			next.ServeHTTP(w, r)
		})
	}
}

// WithoutDeadlines clears the deadlines Deadlines set, for the one route
// that takes over its connection and bounds every read and write itself (the
// WebSocket gateway). Mount it on that route only.
func WithoutDeadlines(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Time{})
		_ = rc.SetWriteDeadline(time.Time{})
		next.ServeHTTP(w, r)
	})
}
