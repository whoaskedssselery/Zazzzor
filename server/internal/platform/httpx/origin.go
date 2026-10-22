package httpx

import (
	"net/http"
	"slices"
	"strings"
)

// RequireOrigin refuses state-changing requests (anything but GET, HEAD and
// OPTIONS) and WebSocket upgrades unless their Origin header is exactly one of
// allowed. Refusals are 403 origin_not_allowed. A missing Origin is refused too.
func RequireOrigin(allowed []string) func(http.Handler) http.Handler {
	allowed = slices.Clone(allowed)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if needsOrigin(r) && !OriginAllowed(allowed, r) {
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
