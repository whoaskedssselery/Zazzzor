package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

func TestPathIDAcceptsOnlyCanonicalIDs(t *testing.T) {
	r := chi.NewRouter()
	r.With(httpx.NoStore).Get("/things/{id}", httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, id)
		return nil
	}))
	for path, want := range map[string]int{
		"/things/123":                  http.StatusOK,
		"/things/0123":                 http.StatusNotFound,
		"/things/+123":                 http.StatusNotFound,
		"/things/0":                    http.StatusNotFound,
		"/things/-5":                   http.StatusNotFound,
		"/things/abc":                  http.StatusNotFound,
		"/things/99999999999999999999": http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s = %d, want %d", path, rec.Code, want)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q", path, got)
		}
	}
}
