package admin

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Mount registers the admin endpoints on r, which must already run
// auth.Middleware:
//
//	GET, PATCH /admin/settings
//	POST /admin/users/{user_id}/password-reset, /disable, /enable
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, requireAdmin)
		r.Get("/admin/settings", httpx.Wrap(s.handleGetSettings))
		r.Patch("/admin/settings", httpx.Wrap(s.handlePatchSettings))
		r.Post("/admin/users/{user_id}/password-reset", httpx.Wrap(s.handlePasswordReset))
		r.Post("/admin/users/{user_id}/disable", httpx.Wrap(s.handleSetDisabled(true)))
		r.Post("/admin/users/{user_id}/enable", httpx.Wrap(s.handleSetDisabled(false)))
	})
}

// requireAdmin answers 403 admin_only to anyone but an administrator.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := auth.FromContext(r.Context()); !ok || !u.IsAdmin {
			httpx.WriteProblem(w, httpx.Forbidden("admin_only"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type settingsBody struct {
	RegistrationOpen bool `json:"registration_open"`
}

type patchSettingsRequest struct {
	RegistrationOpen *bool `json:"registration_open"`
}

func (s *Service) handleGetSettings(w http.ResponseWriter, r *http.Request) error {
	open, err := s.settings.RegistrationOpen(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, settingsBody{RegistrationOpen: open})
	return nil
}

func (s *Service) handlePatchSettings(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[patchSettingsRequest](r)
	if err != nil {
		return err
	}
	var open bool
	if req.RegistrationOpen != nil {
		open, err = s.settings.SetRegistrationOpen(r.Context(), *req.RegistrationOpen)
	} else {
		open, err = s.settings.RegistrationOpen(r.Context())
	}
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, settingsBody{RegistrationOpen: open})
	return nil
}

// targetID is the {user_id} of the path; anything that is not an ID is a
// user that does not exist.
func targetID(r *http.Request) (snowflake.ID, error) {
	id, err := snowflake.Parse(chi.URLParam(r, "user_id"))
	if err != nil || id <= 0 {
		return 0, httpx.NotFound()
	}
	return id, nil
}

type temporaryPasswordBody struct {
	TemporaryPassword string `json:"temporary_password"`
}

func (s *Service) handlePasswordReset(w http.ResponseWriter, r *http.Request) error {
	id, err := targetID(r)
	if err != nil {
		return err
	}
	pw, err := s.ResetPassword(r.Context(), id)
	if err != nil {
		return auth.ToProblem(err)
	}
	httpx.JSON(w, http.StatusOK, temporaryPasswordBody{TemporaryPassword: pw})
	return nil
}

func (s *Service) handleSetDisabled(disabled bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := targetID(r)
		if err != nil {
			return err
		}
		actor, _ := auth.FromContext(r.Context())
		err = s.SetDisabled(r.Context(), actor, id, disabled)
		if errors.Is(err, ErrCannotDisableSelf) {
			return httpx.BadRequest("cannot_disable_self", "administrators cannot disable their own account")
		}
		if err != nil {
			return auth.ToProblem(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}
