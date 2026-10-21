package admin_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/admin"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

type recordingPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *recordingPublisher) Publish(_ context.Context, e events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func (p *recordingPublisher) revokedFor(id snowflake.ID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, e := range p.events {
		if pl, ok := e.Payload.(events.SessionRevokedPayload); ok && e.Type == events.SessionRevoked && pl.UserID == id {
			n += len(pl.SessionIDs)
		}
	}
	return n
}

type env struct {
	t   *testing.T
	pub *recordingPublisher
	h   http.Handler
}

// newEnv wires auth, users and admin like the server does: the registration
// switch of auth reads the admin settings. "boss" is the administrator.
func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, pub: &recordingPublisher{}}
	pool := testkit.Postgres(t)
	settings := admin.NewSettings(pool)
	authSvc := auth.NewService(auth.Options{
		Pool:             pool,
		IDs:              snowflake.NewGenerator(0, time.Now),
		Publisher:        e.pub,
		AdminUsernames:   []string{"boss"},
		CookieSecure:     true,
		RegistrationOpen: settings.RegistrationOpen,
		RegisterLimiter:  ratelimit.New(rate.Inf, 1, time.Minute),
	})
	disk, err := files.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	usersSvc := users.NewService(users.Options{Accounts: authSvc, Files: disk, Publisher: e.pub})
	adminSvc := admin.NewService(settings, authSvc)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithLogger(r.Context(), logger)))
		})
	})
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}))
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Middleware(authSvc))
		auth.Mount(r, authSvc)
		users.Mount(r, usersSvc)
		admin.Mount(r, adminSvc)
	})
	e.h = r
	return e
}

func (e *env) client() *testkit.Client {
	e.t.Helper()
	return testkit.NewClient(e.t, e.h)
}

type userBody struct {
	ID string `json:"id"`
}

type problem struct {
	Code string `json:"code"`
}

func creds(username, password string) map[string]string {
	return map[string]string{"username": username, "password": password}
}

func register(t *testing.T, c *testkit.Client, username, password string) snowflake.ID {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds(username, password))
	if resp.Status != http.StatusCreated {
		t.Fatalf("register %s: status %d: %s", username, resp.Status, resp.Body)
	}
	var u userBody
	resp.JSON(&u)
	id, err := snowflake.Parse(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func expect(t *testing.T, resp *testkit.Response, status int, code string) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
	}
	if code == "" {
		return
	}
	var p problem
	resp.JSON(&p)
	if p.Code != code {
		t.Fatalf("code = %q, want %q", p.Code, code)
	}
}

func adminPaths(id snowflake.ID) [][2]string {
	return [][2]string{
		{http.MethodGet, "/api/v1/admin/settings"},
		{http.MethodPatch, "/api/v1/admin/settings"},
		{http.MethodPost, "/api/v1/admin/users/" + id.String() + "/password-reset"},
		{http.MethodPost, "/api/v1/admin/users/" + id.String() + "/disable"},
		{http.MethodPost, "/api/v1/admin/users/" + id.String() + "/enable"},
	}
}

func TestAdminOnly(t *testing.T) {
	e := newEnv(t)
	boss := e.client()
	bossID := register(t, boss, "boss", "password123")
	user := e.client()
	register(t, user, "alice", "password123")
	anon := e.client()

	for _, op := range adminPaths(bossID) {
		var body any
		if op[0] == http.MethodPatch {
			body = map[string]bool{"registration_open": false}
		}
		expect(t, user.Do(op[0], op[1], body), http.StatusForbidden, "admin_only")
		expect(t, anon.Do(op[0], op[1], body), http.StatusUnauthorized, "unauthenticated")
	}
	// The refused PATCH changed nothing; the boss can read the settings.
	resp := boss.Do(http.MethodGet, "/api/v1/admin/settings", nil)
	var s struct {
		RegistrationOpen bool `json:"registration_open"`
	}
	resp.JSON(&s)
	if resp.Status != http.StatusOK || !s.RegistrationOpen {
		t.Fatalf("settings = %d %+v", resp.Status, s)
	}
	// Bossing someone around still needs the Origin.
	expect(t, boss.Do(http.MethodPatch, "/api/v1/admin/settings", map[string]bool{"registration_open": false},
		testkit.WithoutOrigin()), http.StatusForbidden, "origin_not_allowed")
}

func TestAdminSettings(t *testing.T) {
	e := newEnv(t)
	boss := e.client()
	register(t, boss, "boss", "password123")
	type settings struct {
		RegistrationOpen bool `json:"registration_open"`
	}
	patch := func(body any) settings {
		t.Helper()
		resp := boss.Do(http.MethodPatch, "/api/v1/admin/settings", body)
		if resp.Status != http.StatusOK {
			t.Fatalf("PATCH %v = %d: %s", body, resp.Status, resp.Body)
		}
		var s settings
		resp.JSON(&s)
		return s
	}
	if s := patch(map[string]bool{"registration_open": false}); s.RegistrationOpen {
		t.Fatal("still open")
	}
	if s := patch(map[string]any{}); s.RegistrationOpen {
		t.Fatal("an empty patch reopened registration")
	}
	expect(t, boss.Do(http.MethodPatch, "/api/v1/admin/settings", map[string]any{"registration_open": "yes"}),
		http.StatusBadRequest, "invalid_body")
	if s := patch(map[string]bool{"registration_open": true}); !s.RegistrationOpen {
		t.Fatal("still closed")
	}
}

func TestRegistrationToggle(t *testing.T) {
	e := newEnv(t)
	boss := e.client()
	register(t, boss, "boss", "password123")
	expect(t, boss.Do(http.MethodPatch, "/api/v1/admin/settings", map[string]bool{"registration_open": false}), http.StatusOK, "")

	guest := e.client()
	expect(t, guest.Do(http.MethodPost, "/api/v1/auth/register", creds("guest", "password123")),
		http.StatusForbidden, "registration_closed")
	// Signing in still works while registration is closed.
	expect(t, e.client().Do(http.MethodPost, "/api/v1/auth/login", creds("boss", "password123")), http.StatusOK, "")

	expect(t, boss.Do(http.MethodPatch, "/api/v1/admin/settings", map[string]bool{"registration_open": true}), http.StatusOK, "")
	register(t, guest, "guest", "password123")
}

func TestAdminPasswordReset(t *testing.T) {
	e := newEnv(t)
	boss := e.client()
	register(t, boss, "boss", "password123")
	alice := e.client()
	aliceID := register(t, alice, "alice", "password123")
	alice2 := e.client()
	expect(t, alice2.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", "password123")), http.StatusOK, "")

	resp := boss.Do(http.MethodPost, "/api/v1/admin/users/"+aliceID.String()+"/password-reset", nil)
	expect(t, resp, http.StatusOK, "")
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	var body struct {
		TemporaryPassword string `json:"temporary_password"`
	}
	resp.JSON(&body)
	if !regexp.MustCompile(`^[A-Za-z0-9]{16}$`).MatchString(body.TemporaryPassword) {
		t.Fatalf("temporary_password = %q, want 16 letters and digits", body.TemporaryPassword)
	}

	// Every session of alice is gone and announced.
	expect(t, alice.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	expect(t, alice2.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	if n := e.pub.revokedFor(aliceID); n != 2 {
		t.Fatalf("revoked sessions announced = %d, want 2", n)
	}
	expect(t, alice.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", "password123")),
		http.StatusUnauthorized, "invalid_credentials")
	expect(t, alice.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", body.TemporaryPassword)), http.StatusOK, "")

	// The boss's own session is untouched; a second reset gives a new password.
	again := boss.Do(http.MethodPost, "/api/v1/admin/users/"+aliceID.String()+"/password-reset", nil)
	var body2 struct {
		TemporaryPassword string `json:"temporary_password"`
	}
	again.JSON(&body2)
	if again.Status != http.StatusOK || body2.TemporaryPassword == body.TemporaryPassword {
		t.Fatalf("second reset = %d, same password %v", again.Status, body2.TemporaryPassword == body.TemporaryPassword)
	}

	for _, id := range []string{"12345", "abc", "0", "99999999999999999999"} {
		expect(t, boss.Do(http.MethodPost, "/api/v1/admin/users/"+id+"/password-reset", nil), http.StatusNotFound, "not_found")
	}
}

func TestDisabledUserCannotLogin(t *testing.T) {
	e := newEnv(t)
	boss := e.client()
	bossID := register(t, boss, "boss", "password123")
	alice := e.client()
	aliceID := register(t, alice, "alice", "password123")
	path := "/api/v1/admin/users/" + aliceID.String()

	expect(t, boss.Do(http.MethodPost, path+"/disable", nil), http.StatusNoContent, "")
	expect(t, alice.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	if n := e.pub.revokedFor(aliceID); n != 1 {
		t.Fatalf("revoked sessions announced = %d, want 1", n)
	}
	expect(t, alice.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", "password123")),
		http.StatusForbidden, "account_disabled")
	// Idempotent.
	expect(t, boss.Do(http.MethodPost, path+"/disable", nil), http.StatusNoContent, "")

	expect(t, boss.Do(http.MethodPost, path+"/enable", nil), http.StatusNoContent, "")
	expect(t, alice.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", "password123")), http.StatusOK, "")
	expect(t, boss.Do(http.MethodPost, path+"/enable", nil), http.StatusNoContent, "")

	expect(t, boss.Do(http.MethodPost, "/api/v1/admin/users/"+bossID.String()+"/disable", nil),
		http.StatusBadRequest, "cannot_disable_self")
	expect(t, boss.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusOK, "")
	for _, op := range []string{"/disable", "/enable"} {
		expect(t, boss.Do(http.MethodPost, "/api/v1/admin/users/424242"+op, nil), http.StatusNotFound, "not_found")
		expect(t, boss.Do(http.MethodPost, "/api/v1/admin/users/x"+op, nil), http.StatusNotFound, "not_found")
	}
}
