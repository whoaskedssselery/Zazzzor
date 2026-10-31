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
//	PATCH /users/@me/voice-state
//	DELETE /users/@me/voice
//
// The webhook is separate (MountWebhook): it has no session.
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		writes := r.With(auth.LimitPerUser(s.writeLimiter))
		writes.Post("/channels/{channel_id}/voice", httpx.Wrap(s.handleJoin))
		writes.Patch("/users/@me/voice-state", httpx.Wrap(s.handleUpdateSelf))
		writes.Delete("/users/@me/voice", httpx.Wrap(s.handleLeave))
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

type updateSelfRequest struct {
	SelfMute *bool `json:"self_mute"`
	SelfDeaf *bool `json:"self_deaf"`
}

func (s *Service) handleUpdateSelf(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[updateSelfRequest](r)
	if err != nil {
		return err
	}
	st, err := s.UpdateSelf(r.Context(), auth.CurrentUser(r).ID, SelfFields(req))
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, st.Public())
	return nil
}

func (s *Service) handleLeave(w http.ResponseWriter, r *http.Request) error {
	if err := s.Leave(r.Context(), auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
