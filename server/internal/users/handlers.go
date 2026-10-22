package users

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

// Mount registers the profile endpoints on r, which must already run
// auth.Middleware:
//
//	GET, PATCH /users/@me; POST /users/@me/password; PUT, DELETE /users/@me/avatar
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		r.Get("/users/@me", httpx.Wrap(handleMe))
		r.With(s.limitProfileWrites).Patch("/users/@me", httpx.Wrap(s.handlePatchMe))
		r.Post("/users/@me/password", httpx.Wrap(s.handlePassword))
		r.With(s.limitProfileWrites).Put("/users/@me/avatar", httpx.Wrap(s.handlePutAvatar))
		r.With(s.limitProfileWrites).Delete("/users/@me/avatar", httpx.Wrap(s.handleDeleteAvatar))
	})
}

// limitProfileWrites charges the per-user profile-write limit (shared by
// the display name and avatar routes) before any of the body is read.
func (s *Service) limitProfileWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := s.profileLimiter.Allow(auth.CurrentUser(r).ID.String()); !ok {
			httpx.WriteProblem(w, httpx.TooManyRequests(retry))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleMe(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, auth.CurrentUser(r).Public())
	return nil
}

// patchMeRequest keeps the raw field to tell absent from null.
type patchMeRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
}

func fieldError(field, msg string) error {
	return httpx.BadRequest("validation_failed", "some fields are invalid", httpx.FieldError{Field: field, Message: msg})
}

func (s *Service) handlePatchMe(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[patchMeRequest](r)
	if err != nil {
		return err
	}
	u := auth.CurrentUser(r)
	if req.DisplayName != nil {
		var dn string
		if bytes.Equal(req.DisplayName, []byte("null")) || json.Unmarshal(req.DisplayName, &dn) != nil {
			return fieldError("display_name", "must be a string")
		}
		if u, err = s.UpdateDisplayName(r.Context(), u, dn); err != nil {
			return auth.ToProblem(err)
		}
	}
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Service) handlePassword(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[passwordRequest](r)
	if err != nil {
		return err
	}
	sessionID, _ := auth.SessionFromContext(r.Context())
	err = s.ChangePassword(r.Context(), auth.CurrentUser(r).ID, sessionID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, ErrWrongPassword) {
		return fieldError("current_password", "is incorrect")
	}
	if err != nil {
		return auth.ToProblem(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handlePutAvatar(w http.ResponseWriter, r *http.Request) error {
	upload.ExtendDeadlines(w)
	data, err := upload.ReadFile(w, r, "file", files.MaxImageBytes)
	if err != nil {
		return err
	}
	u, err := s.SetAvatar(r.Context(), auth.CurrentUser(r).ID, data)
	if err != nil {
		return auth.ToProblem(files.ToProblem(err))
	}
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}

func (s *Service) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) error {
	u, err := s.ClearAvatar(r.Context(), auth.CurrentUser(r).ID)
	if err != nil {
		return auth.ToProblem(err)
	}
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}
