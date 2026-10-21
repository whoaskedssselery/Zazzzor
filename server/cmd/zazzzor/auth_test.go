package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newAuthRouter is the production router with the domain modules on a fresh
// database and data directory, logging into the returned buffer. "boss" is
// an administrator.
func newAuthRouter(t *testing.T) (http.Handler, *lockedBuffer) {
	t.Helper()
	return newModulesRouter(t, httpx.DeadlineOptions{})
}

func newModulesRouter(t *testing.T, deadlines httpx.DeadlineOptions) (http.Handler, *lockedBuffer) {
	t.Helper()
	cfg := testConfig()
	cfg.AllowedOrigins = []string{testkit.DefaultOrigin}
	cfg.AdminUsernames = []string{"boss"}
	cfg.DataDir = t.TempDir()
	logs := &lockedBuffer{}
	reg := prometheus.NewRegistry()
	bus := events.NewBus(reg)
	t.Cleanup(bus.Close)
	m, err := newModules(cfg, testkit.Postgres(t), snowflake.NewGenerator(0, time.Now), bus)
	if err != nil {
		t.Fatal(err)
	}
	return newRouter(routerDeps{
		Config:    cfg,
		Logger:    slog.New(slog.NewJSONHandler(logs, nil)),
		Metrics:   newHTTPMetrics(reg),
		Bus:       bus,
		Modules:   m,
		Deadlines: deadlines,
	}), logs
}

func TestRequestsWithoutOriginRejected(t *testing.T) {
	h, _ := newAuthRouter(t)
	c := testkit.NewClient(t, h)
	body := map[string]string{"username": "alice", "password": "password123"}
	for _, path := range []string{"/api/v1/auth/register", "/api/v1/auth/login", "/api/v1/auth/logout"} {
		for name, opt := range map[string]testkit.RequestOption{
			"no origin":      testkit.WithoutOrigin(),
			"foreign origin": testkit.WithOrigin("https://evil.example"),
		} {
			resp := c.Do(http.MethodPost, path, body, opt)
			if resp.Status != http.StatusForbidden {
				t.Fatalf("%s %s = %d, want 403", name, path, resp.Status)
			}
			var p struct{ Code string }
			resp.JSON(&p)
			if p.Code != "origin_not_allowed" {
				t.Fatalf("%s %s: code = %q", name, path, p.Code)
			}
			if len(resp.Header.Values("Set-Cookie")) != 0 {
				t.Fatalf("%s %s set a cookie", name, path)
			}
		}
	}
	// Nothing was registered: the same request with the right Origin succeeds.
	if resp := c.Do(http.MethodPost, "/api/v1/auth/register", body); resp.Status != http.StatusCreated {
		t.Fatalf("register with Origin = %d: %s", resp.Status, resp.Body)
	}
}

func TestPasswordNeverInResponsesOrLogs(t *testing.T) {
	h, logs := newAuthRouter(t)
	c := testkit.NewClient(t, h)
	const pw = "Sup3r-Secret-Passw0rd"
	var bodies [][]byte
	do := func(method, path string, body any) *testkit.Response {
		resp := c.Do(method, path, body)
		bodies = append(bodies, resp.Body)
		return resp
	}

	reg := do(http.MethodPost, "/api/v1/auth/register", map[string]string{"username": "alice", "password": pw})
	var token string
	for _, line := range reg.Header.Values("Set-Cookie") {
		if ck, err := http.ParseSetCookie(line); err == nil && ck.Name == auth.CookieName {
			token = ck.Value
		}
	}
	if token == "" {
		t.Fatalf("no session cookie: %v", reg.Header)
	}
	do(http.MethodPost, "/api/v1/auth/register", map[string]string{"username": "alice", "password": pw})
	do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "alice", "password": pw + "x"})
	do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "alice", "password": pw})
	do(http.MethodGet, "/api/v1/users/@me", nil)
	do(http.MethodPost, "/api/v1/auth/logout", nil)

	for i, b := range bodies {
		for _, secret := range []string{pw, token, "argon2"} {
			if strings.Contains(string(b), secret) {
				t.Fatalf("response %d contains %q: %s", i, secret, b)
			}
		}
	}
	out := logs.String()
	if !strings.Contains(out, `"route":"/api/v1/auth/login"`) {
		t.Fatalf("request log missing; logs = %s", out)
	}
	for _, secret := range []string{pw, token, "argon2", "zz_session"} {
		if strings.Contains(out, secret) {
			t.Fatalf("logs contain %q: %s", secret, out)
		}
	}
}
