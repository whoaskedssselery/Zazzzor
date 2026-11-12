package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// Mount registers the auth endpoints on r, which must already run
// Middleware(s):
//
//	POST /auth/register, POST /auth/login, POST /auth/logout
func Mount(r chi.Router, s *Service) {
	r.Post("/auth/register", httpx.Wrap(s.handleRegister))
	r.Post("/auth/login", httpx.Wrap(s.handleLogin))
	r.Post("/auth/logout", httpx.Wrap(s.handleLogout))
}

type registerRequest struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName *string `json:"display_name"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// clientKey is the rate-limit key of the request (httpx.ClientKey).
func (s *Service) clientKey(r *http.Request) string { return httpx.ClientKey(r, s.trusted) }

func (s *Service) sessionMeta(r *http.Request) SessionMeta {
	return SessionMeta{UserAgent: r.UserAgent(), IP: httpx.ClientIP(r, s.trusted).WithZone("")}
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	req, err := httpx.Decode[registerRequest](r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if err = s.checkRegistrationOpen(ctx); err != nil {
		return ToProblem(err)
	}
	v, err := validateRegistration(RegisterInput(req))
	if err != nil {
		return ToProblem(err)
	}
	// Only well-formed requests reach the limiter: a typo must not cost
	// one of the three registrations an hour.
	if ok, retry := s.registerLimiter.Allow(s.clientKey(r)); !ok {
		return httpx.TooManyRequests(retry)
	}
	u, token, err := s.register(ctx, v, s.sessionMeta(r))
	if err != nil {
		return ToProblem(err)
	}
	s.setSessionCookie(w, token)
	httpx.JSON(w, http.StatusCreated, u.Self())
	return nil
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	if ok, retry := s.loginLimiter.Allow(s.clientKey(r)); !ok {
		return httpx.TooManyRequests(retry)
	}
	req, err := httpx.Decode[loginRequest](r)
	if err != nil {
		return err
	}
	u, token, err := s.Login(r.Context(), req.Username, req.Password, s.sessionMeta(r))
	if err != nil {
		return ToProblem(err)
	}
	s.setSessionCookie(w, token)
	httpx.JSON(w, http.StatusOK, u.Self())
	return nil
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	if ok, retry := s.logoutLimiter.Allow(s.clientKey(r)); !ok {
		return httpx.TooManyRequests(retry)
	}
	if id, ok := SessionFromContext(r.Context()); ok {
		if err := s.RevokeSession(r.Context(), id); err != nil {
			return err
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
