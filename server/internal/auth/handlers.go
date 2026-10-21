package auth

import (
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// Mount registers the auth endpoints on r, which must already run
// Middleware(s):
//
//	POST /auth/register, POST /auth/login, POST /auth/logout, GET /users/@me
func Mount(r chi.Router, s *Service) {
	r.Post("/auth/register", httpx.Wrap(s.handleRegister))
	r.Post("/auth/login", httpx.Wrap(s.handleLogin))
	r.Post("/auth/logout", httpx.Wrap(s.handleLogout))
	// Minimal until the users module takes it over.
	r.With(Require).Get("/users/@me", httpx.Wrap(handleMe))
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

// clientKey is the rate-limit key of the request: an IPv4 client address, or
// the /64 of an IPv6 one (one subscriber usually holds a whole /64, so per
// address limits would be trivial to dodge). Zones are dropped and
// IPv4-mapped addresses count as IPv4.
func (s *Service) clientKey(r *http.Request) string {
	a := httpx.ClientIP(r, s.trusted).WithZone("").Unmap()
	if !a.Is6() {
		return a.String()
	}
	return netip.PrefixFrom(a, 64).Masked().String()
}

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
	httpx.JSON(w, http.StatusCreated, u.Public())
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
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	if id, ok := SessionFromContext(r.Context()); ok {
		if err := s.RevokeSession(r.Context(), id); err != nil {
			return err
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func handleMe(w http.ResponseWriter, r *http.Request) error {
	u, _ := FromContext(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}
