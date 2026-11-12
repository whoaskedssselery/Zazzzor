package invites

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// Mount registers the invite endpoints on r, which must already run
// auth.Middleware:
//
//	POST /channels/{channel_id}/invites; GET /guilds/{guild_id}/invites
//	GET, POST, DELETE /invites/{code}
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		byCode := r.With(auth.LimitPerUser(s.lookupLimiter))

		r.Post("/channels/{channel_id}/invites", httpx.Wrap(s.handleCreate))
		r.Get("/guilds/{guild_id}/invites", httpx.Wrap(s.handleList))
		byCode.Get("/invites/{code}", httpx.Wrap(s.handlePreview))
		byCode.Post("/invites/{code}", httpx.Wrap(s.handleJoin))
		byCode.Delete("/invites/{code}", httpx.Wrap(s.handleDelete))
	})
}

func fail(err error) error { return auth.ToProblem(ToProblem(err)) }

type createRequest struct {
	MaxAgeSeconds *int `json:"max_age_seconds"`
	MaxUses       *int `json:"max_uses"`
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[createRequest](r)
	if err != nil {
		return err
	}
	inv, err := s.Create(r.Context(), id, auth.CurrentUser(r), CreateFields(req))
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusCreated, inv.Public())
	return nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	query := r.URL.Query()
	before := query.Get("before")
	if query.Has("before") && !ValidCode(before) {
		return fail(invalid("before", "must be an invite code"))
	}
	limit := DefaultListed
	if query.Has("limit") {
		limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || limit < 1 || limit > MaxListed {
			return fail(invalid("limit", "must be from 1 to "+strconv.Itoa(MaxListed)))
		}
	}
	invs, err := s.List(r.Context(), id, auth.CurrentUser(r).ID, before, limit)
	if err != nil {
		return fail(err)
	}
	out := make([]PublicInvite, len(invs))
	for i, inv := range invs {
		out[i] = inv.Public()
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (s *Service) handlePreview(w http.ResponseWriter, r *http.Request) error {
	inv, err := s.Preview(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, inv.Public())
	return nil
}

func (s *Service) handleJoin(w http.ResponseWriter, r *http.Request) error {
	g, err := s.Join(r.Context(), chi.URLParam(r, "code"), auth.CurrentUser(r).ID)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, g.Public())
	return nil
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) error {
	if err := s.Delete(r.Context(), chi.URLParam(r, "code"), auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
