package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

func TestRequireOriginBlocksForeignOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := httpx.RequireOrigin([]string{"https://zazzzor.example"})(ok)

	cases := []struct {
		name, method, origin, upgrade string
		status                        int
	}{
		{"foreign POST", http.MethodPost, "https://evil.example", "", http.StatusForbidden},
		{"POST without origin", http.MethodPost, "", "", http.StatusForbidden},
		{"DELETE foreign", http.MethodDelete, "https://evil.example", "", http.StatusForbidden},
		{"PATCH null origin", http.MethodPatch, "null", "", http.StatusForbidden},
		{"allowed POST", http.MethodPost, "https://zazzzor.example", "", http.StatusNoContent},
		{"GET without origin", http.MethodGet, "", "", http.StatusNoContent},
		{"HEAD without origin", http.MethodHead, "", "", http.StatusNoContent},
		{"GET foreign plain", http.MethodGet, "https://evil.example", "", http.StatusNoContent},
		{"websocket upgrade foreign", http.MethodGet, "https://evil.example", "websocket", http.StatusForbidden},
		{"websocket upgrade no origin", http.MethodGet, "", "websocket", http.StatusForbidden},
		{"websocket upgrade allowed", http.MethodGet, "https://zazzzor.example", "websocket", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/x", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.upgrade != "" {
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Upgrade", tc.upgrade)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.status == http.StatusForbidden {
				if p := decodeProblem(t, rec); p.Code != "origin_not_allowed" {
					t.Errorf("code = %q, want origin_not_allowed", p.Code)
				}
			}
		})
	}
}

func TestOriginAllowed(t *testing.T) {
	allowed := []string{"https://zazzzor.example"}
	for origin, want := range map[string]bool{
		"https://zazzzor.example":      true,
		"":                             false,
		"null":                         false,
		"http://zazzzor.example":       false,
		"https://zazzzor.example:443":  false,
		"https://ZAZZZOR.example":      false,
		"https://zazzzor.example.evil": false,
	} {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if got := httpx.OriginAllowed(allowed, r); got != want {
			t.Errorf("OriginAllowed(%q) = %v, want %v", origin, got, want)
		}
	}
}
