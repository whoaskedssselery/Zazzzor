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
			if needsOrigin(r) && !slices.Contains(allowed, r.Header.Get("Origin")) {
				WriteProblem(w, Forbidden("origin_not_allowed"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func needsOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return isWebSocketUpgrade(r)
	default:
		return true
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	for _, v := range r.Header.Values("Upgrade") {
		for token := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "websocket") {
				return true
			}
		}
	}
	return false
}
