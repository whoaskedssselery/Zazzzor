package relationships

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// Mount registers relationship routes on a router with auth.Middleware.
func Mount(r chi.Router, s *Service) {
	const base = "/users/@me/relationships"
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		r.Get(base, httpx.Wrap(s.handleList))
		r.Post(base, httpx.Wrap(s.handleRequest))
		r.Put(base+"/{user_id}", httpx.Wrap(s.handlePut))
		r.Delete(base+"/{user_id}", httpx.Wrap(s.handleDelete))
	})
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	list, err := s.List(r.Context(), auth.CurrentUser(r).ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

func (s *Service) handleRequest(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[struct {
		Username *string `json:"username"`
	}](r)
	if err != nil {
		return err
	}
	if req.Username == nil {
		return httpx.BadRequest("validation_failed", "some fields are invalid",
			httpx.FieldError{Field: "username", Message: "must be a string"})
	}
	if err = s.Request(r.Context(), auth.CurrentUser(r).ID, *req.Username); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handlePut(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "user_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[struct {
		Type Type `json:"type"`
	}](r)
	if err != nil {
		return err
	}
	if err = s.Put(r.Context(), auth.CurrentUser(r).ID, id, req.Type); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "user_id")
	if err != nil {
		return err
	}
	if err = s.Delete(r.Context(), auth.CurrentUser(r).ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
