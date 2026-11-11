package messages_test

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
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
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

type env struct {
	t      *testing.T
	pool   *pgxpool.Pool
	guilds *guilds.Service
	msgs   *messages.Service
	pub    *recorder
	h      http.Handler
	disk   *files.Disk
}

func unlimited() *ratelimit.Limiter { return ratelimit.New(rate.Inf, 1, time.Minute) }

// newEnv serves auth, guilds, channels and messages; the messages service
// gets unlimited send and write limiters (the typing throttle keeps its
// default) unless opts change them.
func newEnv(t *testing.T, opts ...func(*messages.Options)) *env {
	t.Helper()
	e := &env{t: t, pool: testkit.Postgres(t), pub: &recorder{}}
	ids := snowflake.NewGenerator(0, time.Now)
	authSvc := auth.NewService(auth.Options{Pool: e.pool, IDs: ids, CookieSecure: true,
		RegisterLimiter: unlimited(), LoginLimiter: unlimited()})
	disk, err := files.NewDisk(t.TempDir(), files.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.disk = disk
	e.guilds = guilds.NewService(guilds.Options{Pool: e.pool, IDs: ids, Users: authSvc, Files: disk, Publisher: e.pub,
		Hooks:         guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil), Overwrites: channels.NewCleaner()},
		CreateLimiter: unlimited(), WriteLimiter: unlimited()})
	chanSvc := channels.NewService(channels.Options{Pool: e.pool, IDs: ids, Guilds: e.guilds, Publisher: e.pub,
		WriteLimiter: unlimited()})
	o := messages.Options{Pool: e.pool, IDs: ids, Channels: chanSvc, Users: authSvc, Publisher: e.pub,
		Files: disk, SendLimiter: unlimited(), WriteLimiter: unlimited()}
	for _, opt := range opts {
		opt(&o)
	}
	msgSvc := messages.NewService(o)
	e.msgs = msgSvc

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
		guilds.Mount(r, e.guilds)
		channels.Mount(r, chanSvc)
		messages.Mount(r, msgSvc)
	})
	e.h = r
	return e
}

type userBody struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

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
	if err := e.guilds.Join(e.t.Context(), parseID(e.t, g.ID), parseID(e.t, u.ID)); err != nil {
		e.t.Fatalf("join: %v", err)
	}
}

type guildBody struct {
	ID string `json:"id"`
}

type channelBody struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

type stubBody struct {
	ID      string    `json:"id"`
	Author  *userBody `json:"author"`
	Content string    `json:"content"`
}

type messageBody struct {
	ID                string    `json:"id"`
	ChannelID         string    `json:"channel_id"`
	GuildID           string    `json:"guild_id"`
	Author            *userBody `json:"author"`
	Content           string    `json:"content"`
	CreatedAt         string    `json:"created_at"`
	EditedAt          *string   `json:"edited_at"`
	ReplyToID         *string   `json:"reply_to_id"`
	ReferencedMessage *stubBody `json:"referenced_message"`
	Nonce             *string   `json:"nonce"`
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

func perm(p permissions.Permission) string {
	b, _ := p.MarshalJSON()
	return string(b[1 : len(b)-1])
}

func listChannels(t *testing.T, c *testkit.Client, g guildBody) []channelBody {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil)
	expect(t, resp, http.StatusOK)
	var cs []channelBody
	resp.JSON(&cs)
	return cs
}

// newGuild creates a guild and returns it with its default text and voice
// channels.
func newGuild(t *testing.T, c *testkit.Client, name string) (g guildBody, text, voice channelBody) {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": name})
	expect(t, resp, http.StatusCreated)
	resp.JSON(&g)
	for _, ch := range listChannels(t, c, g) {
		switch ch.Type {
		case 0:
			text = ch
		case 2:
			voice = ch
		}
	}
	if text.ID == "" || voice.ID == "" {
		t.Fatal("the new guild lacks a default text or voice channel")
	}
	return g, text, voice
}

func createChannel(t *testing.T, c *testkit.Client, g guildBody, name string) channelBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": name, "type": 0})
	expect(t, resp, http.StatusCreated)
	var ch channelBody
	resp.JSON(&ch)
	return ch
}

// denyEveryone puts a deny overwrite for @everyone on the channel.
func denyEveryone(t *testing.T, c *testkit.Client, g guildBody, channelID string, deny permissions.Permission) {
	t.Helper()
	expect(t, c.Do(http.MethodPut, "/api/v1/channels/"+channelID+"/permissions/"+g.ID,
		map[string]any{"type": 0, "allow": "0", "deny": perm(deny)}), http.StatusOK)
}

func messagesPath(channelID string) string { return "/api/v1/channels/" + channelID + "/messages" }

func messagePath(channelID, messageID string) string {
	return messagesPath(channelID) + "/" + messageID
}

func send(c *testkit.Client, channelID string, body map[string]any) *testkit.Response {
	return c.Do(http.MethodPost, messagesPath(channelID), body)
}

func post(t *testing.T, c *testkit.Client, channelID, content string) messageBody {
	t.Helper()
	resp := send(c, channelID, map[string]any{"content": content})
	expect(t, resp, http.StatusCreated)
	var m messageBody
	resp.JSON(&m)
	return m
}

func list(t *testing.T, c *testkit.Client, channelID, query string) []messageBody {
	t.Helper()
	resp := c.Do(http.MethodGet, messagesPath(channelID)+query, nil)
	expect(t, resp, http.StatusOK)
	var out []messageBody
	resp.JSON(&out)
	return out
}

func ids(ms []messageBody) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
