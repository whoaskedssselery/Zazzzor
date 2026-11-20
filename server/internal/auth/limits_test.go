package auth_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// TestSessionsCappedPerUser: past MaxSessions a new session evicts the
// oldest ones in the same transaction, and their revocation is announced
// so their gateway connections close.
func TestSessionsCappedPerUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *auth.Options) { o.MaxSessions = 3 })
	first := e.client()
	u := register(t, first, "maria", "password123")
	clients := []*testkit.Client{first}
	for range 3 {
		c := e.client()
		if resp := c.Do(http.MethodPost, "/api/v1/auth/login", creds("maria", "password123")); resp.Status != http.StatusOK {
			t.Fatalf("login: %d %s", resp.Status, resp.Body)
		}
		clients = append(clients, c)
	}

	problemOf(t, first.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	for i, c := range clients[1:] {
		if resp := c.Do(http.MethodGet, "/api/v1/users/@me", nil); resp.Status != http.StatusOK {
			t.Fatalf("session %d: %d, want 200", i+2, resp.Status)
		}
	}
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&n); err != nil || n != 3 {
		t.Fatalf("sessions = %d (%v), want 3", n, err)
	}
	var revoked []snowflake.ID
	for _, ev := range e.pub.all() {
		if p, ok := ev.Payload.(events.SessionRevokedPayload); ok && ev.Type == events.SessionRevoked {
			if p.UserID.String() != u.ID {
				t.Fatalf("revocation for %s, want %s", p.UserID, u.ID)
			}
			revoked = append(revoked, p.SessionIDs...)
		}
	}
	if len(revoked) != 1 {
		t.Fatalf("revoked sessions announced = %v, want the oldest one", revoked)
	}
	var left []snowflake.ID
	rows, err := e.pool.Query(t.Context(), "SELECT id FROM sessions")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id snowflake.ID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if rows.Err() != nil || slices.Contains(left, revoked[0]) {
		t.Fatalf("the announced session %s is still stored (%v, %v)", revoked[0], left, rows.Err())
	}
}

func TestLogoutRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *auth.Options) { o.LogoutLimiter = ratelimit.New(rate.Every(time.Hour), 1, time.Hour) })
	c := e.client()
	if resp := c.Do(http.MethodPost, "/api/v1/auth/logout", nil); resp.Status != http.StatusNoContent {
		t.Fatalf("first logout = %d", resp.Status)
	}
	resp := c.Do(http.MethodPost, "/api/v1/auth/logout", nil)
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
}

// TestLimitReadsChargesSignedInGets: only the GETs of a signed-in user
// count, per user.
func TestLimitReadsChargesSignedInGets(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := chi.NewRouter()
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}))
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Middleware(e.svc), auth.LimitReads(ratelimit.New(rate.Every(time.Hour), 2, time.Hour)))
		auth.Mount(r, e.svc)
		r.Get("/users/@me", func(w http.ResponseWriter, r *http.Request) {
			u, ok := auth.FromContext(r.Context())
			if !ok {
				httpx.WriteProblem(w, httpx.Unauthorized("unauthenticated"))
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			httpx.JSON(w, http.StatusOK, u.Self())
		})
	})
	anon := testkit.NewClient(t, r)
	for range 3 {
		problemOf(t, anon.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	}
	alice, bob := testkit.NewClient(t, r), testkit.NewClient(t, r)
	register(t, alice, "alice", "password123")
	register(t, bob, "bob", "password123")
	for range 2 {
		if resp := alice.Do(http.MethodGet, "/api/v1/users/@me", nil); resp.Status != http.StatusOK {
			t.Fatalf("read = %d", resp.Status)
		}
	}
	resp := alice.Do(http.MethodGet, "/api/v1/users/@me", nil)
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	// Writes do not count, and other users have their own budget.
	if resp := alice.Do(http.MethodPost, "/api/v1/auth/logout", nil); resp.Status != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.Status)
	}
	if resp := bob.Do(http.MethodGet, "/api/v1/users/@me", nil); resp.Status != http.StatusOK {
		t.Fatalf("bob's read = %d", resp.Status)
	}
}
