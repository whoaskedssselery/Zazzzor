package guilds_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
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

// recorder publishes to a real bus and remembers every event.
type recorder struct {
	bus    *events.Bus
	mu     sync.Mutex
	events []events.Event
}

func (p *recorder) Publish(ctx context.Context, e events.Event) {
	p.mu.Lock()
	p.events = append(p.events, e)
	p.mu.Unlock()
	p.bus.Publish(ctx, e)
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

// channelsHook records the guilds it bootstrapped and can fail.
type channelsHook struct {
	mu    sync.Mutex
	calls []snowflake.ID
	fail  bool
	// inTx is set when the guild row was visible inside the hook's tx.
	inTx bool
}

func (h *channelsHook) CreateDefaultChannels(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, guildID)
	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM guilds WHERE id = $1", int64(guildID)).Scan(&n); err != nil {
		return err
	}
	h.inTx = n == 1
	if h.fail {
		return errors.New("channels: boom")
	}
	return nil
}

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	auth  *auth.Service
	svc   *guilds.Service
	disk  *files.Disk
	dir   string
	bus   *events.Bus
	pub   *recorder
	hook  *channelsHook
	h     http.Handler
	seq   int
	seqMu sync.Mutex
}

func unlimited() *ratelimit.Limiter { return ratelimit.New(rate.Inf, 1, time.Minute) }

func newEnv(t *testing.T, opts ...func(*guilds.Options)) *env {
	t.Helper()
	return newEnvOn(t, testkit.Postgres(t), opts...)
}

// newEnvOn is newEnv over a given pool (e.g. a deliberately small one).
func newEnvOn(t *testing.T, pool *pgxpool.Pool, opts ...func(*guilds.Options)) *env {
	t.Helper()
	e := &env{t: t, pool: pool, dir: t.TempDir(), bus: events.NewBus(nil), hook: &channelsHook{}}
	t.Cleanup(e.bus.Close)
	e.pub = &recorder{bus: e.bus}
	ids := snowflake.NewGenerator(0, time.Now)
	e.auth = auth.NewService(auth.Options{
		Pool: e.pool, IDs: ids, CookieSecure: true,
		RegisterLimiter: unlimited(), LoginLimiter: unlimited(),
	})
	var err error
	if e.disk, err = files.NewDisk(e.dir, files.Options{}); err != nil {
		t.Fatal(err)
	}
	o := guilds.Options{Pool: e.pool, IDs: ids, Users: e.auth, Files: e.disk, Publisher: e.pub,
		Hooks:         guilds.Hooks{Channels: e.hook},
		CreateLimiter: unlimited(), WriteLimiter: unlimited()}
	for _, opt := range opts {
		opt(&o)
	}
	e.svc = guilds.NewService(o)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithLogger(r.Context(), logger)))
		})
	})
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}))
	r.Route("/api/v1", func(r chi.Router) {
		files.Mount(r, e.disk)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(e.auth))
			auth.Mount(r, e.auth)
			guilds.Mount(r, e.svc)
		})
	})
	e.h = r
	return e
}

type userBody struct {
	ID       string `json:"id"`
	Username string `json:"username"`
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

// user registers a new account and returns a client signed in as it.
func (e *env) user(name string) (*testkit.Client, userBody) {
	e.t.Helper()
	c := testkit.NewClient(e.t, e.h)
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", map[string]string{"username": name, "password": "password123"})
	if resp.Status != http.StatusCreated {
		e.t.Fatalf("register %s: %d %s", name, resp.Status, resp.Body)
	}
	var u userBody
	resp.JSON(&u)
	return c, u
}

// rawUsers inserts n accounts straight into the database (no password
// hashing) and returns their IDs; they cannot sign in.
func (e *env) rawUsers(n int) []snowflake.ID {
	e.t.Helper()
	ids := snowflake.NewGenerator(1, time.Now)
	out := make([]snowflake.ID, n)
	for i := range n {
		e.seqMu.Lock()
		e.seq++
		name := "raw" + strconv.Itoa(e.seq)
		e.seqMu.Unlock()
		out[i] = ids.Next()
		if _, err := e.pool.Exec(e.t.Context(),
			"INSERT INTO users (id, username, display_name, password_hash, created_at) VALUES ($1, $2, $2, 'x', now())",
			int64(out[i]), name); err != nil {
			e.t.Fatal(err)
		}
	}
	return out
}

func (e *env) join(guildID, userID snowflake.ID) {
	e.t.Helper()
	if err := e.svc.Join(e.t.Context(), guildID, userID); err != nil {
		e.t.Fatalf("join: %v", err)
	}
}

type guildBody struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	IconURL *string `json:"icon_url"`
	OwnerID string  `json:"owner_id"`
}

func (g guildBody) id(t *testing.T) snowflake.ID { return parseID(t, g.ID) }

type roleBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       int    `json:"color"`
	Position    int    `json:"position"`
	Permissions string `json:"permissions"`
	Hoist       bool   `json:"hoist"`
	Mentionable bool   `json:"mentionable"`
}

type memberBody struct {
	User     userBody `json:"user"`
	Nick     *string  `json:"nick"`
	Roles    []string `json:"roles"`
	JoinedAt string   `json:"joined_at"`
}

type problem struct {
	Status int    `json:"status"`
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

func createRole(t *testing.T, c *testkit.Client, g guildBody, name string, p permissions.Permission) roleBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles", map[string]any{"name": name, "permissions": perm(p)})
	expect(t, resp, http.StatusCreated)
	var r roleBody
	resp.JSON(&r)
	return r
}

func setRoles(c *testkit.Client, g guildBody, userID string, roles ...string) *testkit.Response {
	if roles == nil {
		roles = []string{}
	}
	return c.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+userID, map[string]any{"roles": roles})
}

func listRoles(t *testing.T, c *testkit.Client, g guildBody) []roleBody {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/roles", nil)
	expect(t, resp, http.StatusOK)
	var rs []roleBody
	resp.JSON(&rs)
	return rs
}

func pngImage(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func uploadIcon(t *testing.T, c *testkit.Client, g guildBody, content []byte) *testkit.Response {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", "icon.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(content)
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return c.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/icon", b.Bytes(), testkit.WithHeader("Content-Type", w.FormDataContentType()))
}
