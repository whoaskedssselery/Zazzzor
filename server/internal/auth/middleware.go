package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
)

// Middleware resolves the session cookie and, when it is valid, stores the
// user and session ID in the request context (see FromContext). Requests
// without a valid session pass through anonymous; Require rejects them where
// a session is needed. A session last renewed a day or more ago is renewed
// and its cookie re-sent; a failed renewal is logged and the request goes
// on.
func Middleware(s *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ck, err := r.Cookie(CookieName)
			if err != nil || ck.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			sess, err := s.lookup(ctx, ck.Value)
			if errors.Is(err, ErrUnauthenticated) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				// Not "logged out": the database failed. Do not let the
				// client mistake it for a 401.
				httpx.LoggerFrom(ctx).ErrorContext(ctx, "authenticate request", "err", err.Error())
				httpx.WriteProblem(w, httpx.Internal())
				return
			}
			renewed, err := s.renew(ctx, sess)
			if err != nil {
				httpx.LoggerFrom(ctx).WarnContext(ctx, "renew session", "session_id", sess.id.String(), "err", err.Error())
			} else if renewed {
				s.setSessionCookie(w, ck.Value)
			}
			ctx = context.WithValue(ctx, userKey, sess.user)
			ctx = context.WithValue(ctx, sessionKey, sess.id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Require answers 401 unauthenticated unless Middleware found a session.
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			httpx.WriteProblem(w, httpx.Unauthorized("unauthenticated"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FromContext returns the user Middleware stored for this request.
func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	return u, ok
}

// SessionFromContext returns the ID of the session that authenticated this
// request.
func SessionFromContext(ctx context.Context) (snowflake.ID, bool) {
	id, ok := ctx.Value(sessionKey).(snowflake.ID)
	return id, ok
}
