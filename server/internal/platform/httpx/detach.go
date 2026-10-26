package httpx

import (
	"context"
	"net/http"
	"time"
)

// Detach keeps a request that may write (anything but GET, HEAD and
// OPTIONS) running when its client goes away: its context is no longer
// cancelled with the connection, only after timeout (DefaultWriteDeadline
// when 0; the response cannot be written later anyway). Otherwise a write
// the database commits just as the client leaves would fail in the handler
// and never be announced, and a retry would repeat it. Reads stay
// cancellable, so nobody waits on a query nobody reads.
func Detach(timeout time.Duration) func(http.Handler) http.Handler {
	if timeout <= 0 {
		timeout = DefaultWriteDeadline
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
