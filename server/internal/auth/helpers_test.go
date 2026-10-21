package auth_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

// recordingPublisher keeps every published event.
type recordingPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *recordingPublisher) Publish(_ context.Context, e events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func (p *recordingPublisher) all() []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]events.Event(nil), p.events...)
}

// clock is a settable time source; zero offset means real time.
type clock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	svc   *auth.Service
	pub   *recordingPublisher
	clock *clock
	logs  *syncBuffer
	h     http.Handler
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type envOption func(*auth.Options)

func newEnv(t *testing.T, opts ...envOption) *env {
	t.Helper()
	e := &env{t: t, pool: testkit.Postgres(t), pub: &recordingPublisher{}, clock: &clock{}, logs: &syncBuffer{}}
	o := auth.Options{
		Pool:           e.pool,
		IDs:            snowflake.NewGenerator(0, time.Now),
		Publisher:      e.pub,
		AdminUsernames: []string{"boss"},
		CookieSecure:   true,
		Now:            e.clock.Now,
	}
	for _, opt := range opts {
		opt(&o)
	}
	e.svc = auth.NewService(o)

	logger := slog.New(slog.NewJSONHandler(e.logs, nil))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithLogger(r.Context(), logger)))
		})
	})
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}))
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Middleware(e.svc))
		auth.Mount(r, e.svc)
	})
	e.h = r
	return e
}

func (e *env) client() *testkit.Client {
	e.t.Helper()
	return testkit.NewClient(e.t, e.h)
}

type userBody struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   string  `json:"created_at"`
}

type problem struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func creds(username, password string) map[string]string {
	return map[string]string{"username": username, "password": password}
}

// register creates an account through the API and fails the test otherwise.
func register(t *testing.T, c *testkit.Client, username, password string) userBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds(username, password))
	if resp.Status != http.StatusCreated {
		t.Fatalf("register %s: status %d: %s", username, resp.Status, resp.Body)
	}
	var u userBody
	resp.JSON(&u)
	return u
}

func problemOf(t *testing.T, resp *testkit.Response, status int, code string) problem {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
	}
	var p problem
	resp.JSON(&p)
	if p.Code != code {
		t.Fatalf("code = %q, want %q", p.Code, code)
	}
	return p
}

func sessionCookie(t *testing.T, resp *testkit.Response) *http.Cookie {
	t.Helper()
	for _, line := range resp.Header.Values("Set-Cookie") {
		c, err := http.ParseSetCookie(line)
		if err != nil {
			t.Fatalf("parse Set-Cookie %q: %v", line, err)
		}
		if c.Name == "zz_session" {
			return c
		}
	}
	t.Fatalf("no zz_session cookie in %v", resp.Header.Values("Set-Cookie"))
	return nil
}

// cps builds a string from code points, keeping invisible characters out of
// the source.
func cps(r ...rune) string { return string(r) }
