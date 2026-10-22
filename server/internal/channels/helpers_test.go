package channels_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

// recorder remembers every event published.
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
	t      *testing.T
	auth   *auth.Service
	guilds *guilds.Service
	svc    *channels.Service
	pub    *recorder
	h      http.Handler
}

func unlimited() *ratelimit.Limiter { return ratelimit.New(rate.Inf, 1, time.Minute) }

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testkit.Postgres(t)
	e := &env{t: t, pub: &recorder{}}
	ids := snowflake.NewGenerator(0, time.Now)
	e.auth = auth.NewService(auth.Options{Pool: pool, IDs: ids, CookieSecure: true,
		RegisterLimiter: unlimited(), LoginLimiter: unlimited()})
	disk, err := files.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.guilds = guilds.NewService(guilds.Options{Pool: pool, IDs: ids, Users: e.auth, Files: disk, Publisher: e.pub,
		Hooks:         guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil)},
		CreateLimiter: unlimited(), WriteLimiter: unlimited()})
	e.svc = channels.NewService(channels.Options{Pool: pool, IDs: ids, Guilds: e.guilds, Publisher: e.pub,
		WriteLimiter: unlimited()})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithLogger(r.Context(), logger)))
		})
	})
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}))
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Middleware(e.auth))
		auth.Mount(r, e.auth)
		guilds.Mount(r, e.guilds)
		channels.Mount(r, e.svc)
	})
	e.h = r
	return e
}

type userBody struct {
	ID string `json:"id"`
}

func (u userBody) id(t *testing.T) snowflake.ID { return parseID(t, u.ID) }

func parseID(t *testing.T, s string) snowflake.ID {
	t.Helper()
	id, err := snowflake.Parse(s)
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

func (e *env) join(g guildBody, u userBody) {
	e.t.Helper()
	if err := e.guilds.Join(e.t.Context(), g.id(e.t), u.id(e.t)); err != nil {
		e.t.Fatalf("join: %v", err)
	}
}

type guildBody struct {
	ID string `json:"id"`
}

func (g guildBody) id(t *testing.T) snowflake.ID { return parseID(t, g.ID) }

type overwriteBody struct {
	ID    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type channelBody struct {
	ID                   string          `json:"id"`
	GuildID              string          `json:"guild_id"`
	Type                 int             `json:"type"`
	Name                 string          `json:"name"`
	Topic                *string         `json:"topic"`
	ParentID             *string         `json:"parent_id"`
	Position             int             `json:"position"`
	PermissionSynced     bool            `json:"permission_synced"`
	PermissionOverwrites []overwriteBody `json:"permission_overwrites"`
}

func (c channelBody) id(t *testing.T) snowflake.ID { return parseID(t, c.ID) }

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

func createGuild(t *testing.T, c *testkit.Client, name string) guildBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": name})
	expect(t, resp, http.StatusCreated)
	var g guildBody
	resp.JSON(&g)
	return g
}

func perm(p permissions.Permission) string {
	b, _ := p.MarshalJSON()
	return string(b[1 : len(b)-1])
}

// createRole creates a role and returns its ID.
func createRole(t *testing.T, c *testkit.Client, g guildBody, name string, p permissions.Permission) string {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles", map[string]any{"name": name, "permissions": perm(p)})
	expect(t, resp, http.StatusCreated)
	var r struct {
		ID string `json:"id"`
	}
	resp.JSON(&r)
	return r.ID
}

func setRoles(t *testing.T, c *testkit.Client, g guildBody, userID string, roles ...string) {
	t.Helper()
	if roles == nil {
		roles = []string{}
	}
	expect(t, c.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+userID, map[string]any{"roles": roles}), http.StatusOK)
}

func listChannels(t *testing.T, c *testkit.Client, g guildBody) []channelBody {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil)
	expect(t, resp, http.StatusOK)
	var cs []channelBody
	resp.JSON(&cs)
	return cs
}

func byName(t *testing.T, cs []channelBody, name string) channelBody {
	t.Helper()
	i := slices.IndexFunc(cs, func(c channelBody) bool { return c.Name == name })
	if i < 0 {
		t.Fatalf("no channel %q in %+v", name, cs)
	}
	return cs[i]
}

func hasChannel(cs []channelBody, id string) bool {
	return slices.ContainsFunc(cs, func(c channelBody) bool { return c.ID == id })
}

func createChannel(t *testing.T, c *testkit.Client, g guildBody, body map[string]any) channelBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", body)
	expect(t, resp, http.StatusCreated)
	var ch channelBody
	resp.JSON(&ch)
	return ch
}

func putOverwrite(c *testkit.Client, channelID, targetID string, typ int, allow, deny permissions.Permission) *testkit.Response {
	return c.Do(http.MethodPut, "/api/v1/channels/"+channelID+"/permissions/"+targetID,
		map[string]any{"type": typ, "allow": perm(allow), "deny": perm(deny)})
}

func getChannel(t *testing.T, c *testkit.Client, id string) channelBody {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/channels/"+id, nil)
	expect(t, resp, http.StatusOK)
	var ch channelBody
	resp.JSON(&ch)
	return ch
}
