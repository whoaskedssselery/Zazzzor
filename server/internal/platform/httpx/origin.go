package httpx

import (
	"net/http"
	"slices"
	"strings"
)

// RequireOrigin refuses state-changing requests (anything but GET, HEAD and
// OPTIONS) and WebSocket upgrades unless their Origin header is exactly one of
// allowed. Refusals are 403 origin_not_allowed. A missing Origin is refused too.
// exempt lists routes that need no Origin, as "METHOD /path" matched exactly
// against the request's method and unescaped path (server-to-server calls
// that authenticate otherwise, such as signed webhooks).
func RequireOrigin(allowed []string, exempt ...string) func(http.Handler) http.Handler {
	allowed = slices.Clone(allowed)
	exempt = slices.Clone(exempt)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if needsOrigin(r) && !OriginAllowed(allowed, r) && !isExempt(exempt, r) {
				WriteProblem(w, Forbidden("origin_not_allowed"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OriginAllowed reports whether the Origin header of r is exactly one of
// allowed (scheme, host and port, compared byte for byte). A missing Origin
// is not allowed.
func OriginAllowed(allowed []string, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin != "" && slices.Contains(allowed, origin)
}

// isExempt reports whether r is one of the exempt routes; an escaped path
// never is.
func isExempt(exempt []string, r *http.Request) bool {
	return r.URL.RawPath == "" && slices.Contains(exempt, r.Method+" "+r.URL.Path)
}

func needsOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return IsWebSocketUpgrade(r)
	default:
		return true
	}
}

// IsWebSocketUpgrade reports whether r asks for a WebSocket upgrade (an
// Upgrade header naming websocket).
func IsWebSocketUpgrade(r *http.Request) bool {
	for _, v := range r.Header.Values("Upgrade") {
		for token := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "websocket") {
				return true
			}
		}
	}
	return false
}
