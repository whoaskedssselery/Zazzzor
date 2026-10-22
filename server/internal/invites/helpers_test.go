package invites_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/invites"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

type recorder struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *recorder) Publish(_ context.Context, e events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func (p *recorder) ofType(typ string) []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []events.Event
	for _, e := range p.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func (p *recorder) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	pub  *recorder
	h    http.Handler
}

func unlimited() *ratelimit.Limiter { return ratelimit.New(rate.Inf, 1, time.Minute) }

func newEnv(t *testing.T, opts ...func(*invites.Options)) *env {
	t.Helper()
	e := &env{t: t, pool: testkit.Postgres(t), pub: &recorder{}}
	ids := snowflake.NewGenerator(0, time.Now)
	authSvc := auth.NewService(auth.Options{Pool: e.pool, IDs: ids, CookieSecure: true,
		RegisterLimiter: unlimited(), LoginLimiter: unlimited()})
	disk, err := files.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	guildSvc := guilds.NewService(guilds.Options{Pool: e.pool, IDs: ids, Users: authSvc, Files: disk, Publisher: e.pub,
		Hooks:         guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil), Overwrites: channels.NewCleaner()},
		CreateLimiter: unlimited(), WriteLimiter: unlimited()})
	chanSvc := channels.NewService(channels.Options{Pool: e.pool, IDs: ids, Guilds: guildSvc, Publisher: e.pub,
		WriteLimiter: unlimited()})
	o := invites.Options{Pool: e.pool, Channels: chanSvc, Guilds: guildSvc, Users: authSvc,
		CreateLimiter: unlimited(), LookupLimiter: unlimited()}
	for _, opt := range opts {
		opt(&o)
	}
	invSvc := invites.NewService(o)

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
		guilds.Mount(r, guildSvc)
		channels.Mount(r, chanSvc)
		invites.Mount(r, invSvc)
	})
	e.h = r
	return e
}

type userBody struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (u userBody) id(t *testing.T) snowflake.ID {
	t.Helper()
	id, err := snowflake.Parse(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) user(name string) (*testkit.Client, userBody) {
	e.t.Helper()
	c := testkit.NewClient(e.t, e.h)
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", map[string]string{"username": name, "password": "password123"})
	expect(e.t, resp, http.StatusCreated)
	var u userBody
	resp.JSON(&u)
	return c, u
}

type guildBody struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type channelBody struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

type inviteBody struct {
	Code  string `json:"code"`
	Guild struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		IconURL *string `json:"icon_url"`
	} `json:"guild"`
	Channel struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
	Inviter   userBody `json:"inviter"`
	Uses      int      `json:"uses"`
	MaxUses   int      `json:"max_uses"`
	ExpiresAt *string  `json:"expires_at"`
}

type problem struct {
	Code   string `json:"code"`
	Errors []struct {
		Field string `json:"field"`
	} `json:"errors"`
}

func expect(t *testing.T, resp *testkit.Response, status int) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
	}
}

func problemOf(t *testing.T, resp *testkit.Response, status int, code string) problem {
	t.Helper()
	expect(t, resp, status)
	var p problem
	resp.JSON(&p)
	if p.Code != code {
		t.Fatalf("code = %q, want %q", p.Code, code)
	}
	return p
}

// newGuild creates a guild and returns it with its default text channel.
func newGuild(t *testing.T, c *testkit.Client, name string) (guildBody, channelBody) {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": name})
	expect(t, resp, http.StatusCreated)
	var g guildBody
	resp.JSON(&g)
	resp = c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil)
	expect(t, resp, http.StatusOK)
	var cs []channelBody
	resp.JSON(&cs)
	for _, ch := range cs {
		if ch.Type == 0 {
			return g, ch
		}
	}
	t.Fatalf("no text channel in %+v", cs)
	return g, channelBody{}
}

func createInvite(t *testing.T, c *testkit.Client, channelID string, body map[string]any) inviteBody {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	resp := c.Do(http.MethodPost, "/api/v1/channels/"+channelID+"/invites", body)
	expect(t, resp, http.StatusCreated)
	var inv inviteBody
	resp.JSON(&inv)
	return inv
}

func listInvites(t *testing.T, c *testkit.Client, g guildBody) []inviteBody {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/invites", nil)
	expect(t, resp, http.StatusOK)
	var out []inviteBody
	resp.JSON(&out)
	return out
}

func join(c *testkit.Client, code string) *testkit.Response {
	return c.Do(http.MethodPost, "/api/v1/invites/"+code, nil)
}
