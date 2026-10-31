package voice

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// Mount registers the voice endpoints of members on r, which must already
// run auth.Middleware:
//
//	POST /channels/{channel_id}/voice
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		writes := r.With(auth.LimitPerUser(s.writeLimiter))
		writes.Post("/channels/{channel_id}/voice", httpx.Wrap(s.handleJoin))
	})
}

func fail(err error) error { return auth.ToProblem(ToProblem(err)) }

func (s *Service) handleJoin(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	j, err := s.Join(r.Context(), id, auth.CurrentUser(r), r.Header.Get("Origin"))
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, j)
	return nil
}
