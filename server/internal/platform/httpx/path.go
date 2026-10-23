package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// PathID parses the path parameter name as a canonical snowflake: positive
// and written exactly as ID.String() writes it. Anything else is 404, so a
// malformed ID looks like a missing resource.
func PathID(r *http.Request, name string) (snowflake.ID, error) {
	raw := chi.URLParam(r, name)
	id, err := snowflake.Parse(raw)
	if err != nil || id <= 0 || id.String() != raw {
		return 0, NotFound()
	}
	return id, nil
}

// NoStore marks every response as not cacheable (Cache-Control: no-store),
// for routes that return private data.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
