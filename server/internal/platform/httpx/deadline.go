package httpx

import (
	"bufio"
	"fmt"
	"net"
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
// request gets them, whatever its headers. A successful gateway upgrade
// clears them on its hijacked connection through WithoutDeadlines.
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

// WithoutDeadlines clears deadlines only after next hijacks its connection.
// Rejected handshakes and ordinary HTTP requests retain their deadlines.
// Mount it only on a route that bounds its own hijacked reads and writes.
func WithoutDeadlines(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&hijackDeadlineWriter{ResponseWriter: w}, r)
	})
}

// hijackDeadlineWriter preserves HTTP limits until ownership leaves net/http.
type hijackDeadlineWriter struct{ http.ResponseWriter }

// Unwrap lets response controllers reach the original writer's capabilities.
func (w *hijackDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack delegates through middleware and clears only the returned connection.
func (w *hijackDeadlineWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("httpx: hijack connection: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("httpx: clear hijacked deadlines: %w", err)
	}
	return conn, rw, nil
}
