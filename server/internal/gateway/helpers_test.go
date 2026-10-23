package gateway_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

// wait bounds every wait for a frame or a state.
const wait = 5 * time.Second

// env is the production wiring (app.NewModules, app.Routes, the gateway)
// on a fresh database.
type env struct {
	t   *testing.T
	m   *app.Modules
	bus *events.Bus
	gw  *gateway.Gateway
	reg *prometheus.Registry
	h   http.Handler
}

func newEnv(t *testing.T, opts gateway.Options) *env {
	t.Helper()
	cfg, err := config.Load(func(k string) string {
		return map[string]string{
			"ZAZZZOR_DATABASE_URL":    "postgres://test",
			"ZAZZZOR_ALLOWED_ORIGINS": testkit.DefaultOrigin,
		}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir = t.TempDir()
	e := &env{t: t, reg: prometheus.NewRegistry()}
	e.bus = events.NewBus(e.reg)
	t.Cleanup(e.bus.Close)
	if e.m, err = app.NewModules(cfg, testkit.Postgres(t), snowflake.NewGenerator(0, time.Now), e.bus); err != nil {
		t.Fatal(err)
	}
	// Tests dial often from one address and user; the limit tests pass
	// their own limiters.
	if opts.UserUpgradeLimiter == nil {
		opts.UserUpgradeLimiter = ratelimit.New(rate.Inf, 1, time.Minute)
	}
	if opts.IPUpgradeLimiter == nil {
		opts.IPUpgradeLimiter = ratelimit.New(rate.Inf, 1, time.Minute)
	}
	logger := slog.New(slog.DiscardHandler)
	e.gw = e.m.NewGateway(e.bus, cfg, logger, e.reg, opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		if err := e.gw.Close(ctx); err != nil {
			t.Errorf("gateway close: %v", err)
		}
		// Nothing in these tests should fail to resolve, a shutdown included.
		if n := e.metric("zazzzor_gateway_dispatch_errors_total", ""); n != 0 {
			t.Errorf("dispatch errors = %v", n)
		}
	})

	r := chi.NewRouter()
	r.Use(httpx.Deadlines(httpx.DeadlineOptions{}), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithLogger(r.Context(), logger)))
		})
	}, httpx.RequireOrigin(cfg.AllowedOrigins))
	r.Route("/api/v1", func(r chi.Router) { e.m.Routes(r, e.gw) })
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

// user registers name on its own client (at most 3 per env: the default
// registration limit per IP).
func (e *env) user(name string) (*testkit.Client, userBody) {
	e.t.Helper()
	c := testkit.NewClient(e.t, e.h)
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", map[string]string{"username": name, "password": "password123"})
	expect(e.t, resp, http.StatusCreated)
	var u userBody
	resp.JSON(&u)
	return c, u
}

// login signs name in on a new client (a second session).
func (e *env) login(name string) *testkit.Client {
	e.t.Helper()
	c := testkit.NewClient(e.t, e.h)
	expect(e.t, c.Do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": name, "password": "password123"}), http.StatusOK)
	return c
}

func (e *env) join(g guildBody, u userBody) {
	e.t.Helper()
	if err := e.m.Guilds.Join(e.t.Context(), parseID(e.t, g.ID), u.id(e.t)); err != nil {
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

type messageBody struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

func expect(t *testing.T, resp *testkit.Response, status int) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
	}
}

func perm(p permissions.Permission) string {
	b, _ := p.MarshalJSON()
	return string(b[1 : len(b)-1])
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
	t.Fatal("the new guild has no text channel")
	return g, channelBody{}
}

func createChannel(t *testing.T, c *testkit.Client, g guildBody, name string) channelBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": name, "type": 0})
	expect(t, resp, http.StatusCreated)
	var ch channelBody
	resp.JSON(&ch)
	return ch
}

// overwrite sets a channel overwrite (typ 0 role, 1 member).
func overwrite(t *testing.T, c *testkit.Client, channelID, targetID string, typ int, allow, deny permissions.Permission) {
	t.Helper()
	expect(t, c.Do(http.MethodPut, "/api/v1/channels/"+channelID+"/permissions/"+targetID,
		map[string]any{"type": typ, "allow": perm(allow), "deny": perm(deny)}), http.StatusOK)
}

// hiddenChannel creates a channel @everyone may not view.
func hiddenChannel(t *testing.T, owner *testkit.Client, g guildBody, name string) channelBody {
	t.Helper()
	ch := createChannel(t, owner, g, name)
	overwrite(t, owner, ch.ID, g.ID, 0, 0, permissions.ViewChannel)
	return ch
}

func post(t *testing.T, c *testkit.Client, channelID, content string) messageBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/channels/"+channelID+"/messages", map[string]any{"content": content})
	expect(t, resp, http.StatusCreated)
	var m messageBody
	resp.JSON(&m)
	return m
}

// frame is a server frame as received.
type frame struct {
	Op int             `json:"op"`
	T  string          `json:"t"`
	S  int64           `json:"s"`
	D  json.RawMessage `json:"d"`
	// hasS tells s:0 from no s.
	hasS bool
}

// wsClient is a gateway connection whose frames are read in the
// background and checked against the contract as the test consumes them.
type wsClient struct {
	t      *testing.T
	ws     *websocket.Conn
	frames chan []byte

	mu  sync.Mutex
	err error // why reading stopped; set before frames is closed
}

func dialURL(c *testkit.Client) string {
	return "wss" + strings.TrimPrefix(c.URL(), "https") + "/api/v1/gateway"
}

// dialRaw opens a gateway connection without reading it; status is the
// HTTP status of the handshake response (0 without one).
func dialRaw(t *testing.T, c *testkit.Client, origin string) (ws *websocket.Conn, status int, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, dialURL(c), &websocket.DialOptions{
		HTTPClient: c.HTTPClient(),
		HTTPHeader: http.Header{"Origin": {origin}},
	})
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if err == nil {
		t.Cleanup(func() { _ = ws.CloseNow() })
	}
	return ws, status, err
}

// dial connects c and consumes HELLO; READY is the next frame.
func dial(t *testing.T, c *testkit.Client) *wsClient {
	t.Helper()
	ws, _, err := dialRaw(t, c, testkit.DefaultOrigin)
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	ws.SetReadLimit(1 << 24)
	cl := &wsClient{t: t, ws: ws, frames: make(chan []byte, 4096)}
	go func() {
		defer close(cl.frames)
		for {
			_, b, err := ws.Read(context.Background())
			if err != nil {
				cl.mu.Lock()
				cl.err = err
				cl.mu.Unlock()
				return
			}
			cl.frames <- b
		}
	}()
	if f := cl.next(); f.Op != gateway.OpHello {
		t.Fatalf("first frame op = %d, want HELLO", f.Op)
	}
	return cl
}

// connect dials c and returns the client with its READY payload.
func connect(t *testing.T, c *testkit.Client) (*wsClient, readyBody) {
	t.Helper()
	cl := dial(t, c)
	f := cl.next()
	if f.Op != gateway.OpDispatch || f.T != gateway.EventReady || f.S != 1 {
		t.Fatalf("second frame = op %d t %q s %d, want READY s 1", f.Op, f.T, f.S)
	}
	var r readyBody
	decode(t, f.D, &r)
	return cl, r
}

func decode(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// next returns the next frame, failing on a timeout, a closed connection
// or a frame the contract does not describe.
func (c *wsClient) next() frame {
	c.t.Helper()
	f, ok := c.nextWithin(wait)
	if !ok {
		c.t.Fatalf("no frame within %v (connection error: %v)", wait, c.readErr())
	}
	return f
}

func (c *wsClient) nextWithin(d time.Duration) (frame, bool) {
	c.t.Helper()
	select {
	case b, ok := <-c.frames:
		if !ok {
			return frame{}, false
		}
		if err := testkit.CheckGatewayFrame(b); err != nil {
			c.t.Fatalf("frame breaks the contract: %v", err)
		}
		var f frame
		decode(c.t, b, &f)
		var probe map[string]json.RawMessage
		decode(c.t, b, &probe)
		_, f.hasS = probe["s"]
		return f, true
	case <-time.After(d):
		return frame{}, false
	}
}

// waitFor skips frames until a DISPATCH of type typ and returns it.
func (c *wsClient) waitFor(typ string) frame {
	c.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		f, ok := c.nextWithin(time.Until(deadline))
		if !ok {
			c.t.Fatalf("no %s within %v (connection error: %v)", typ, wait, c.readErr())
		}
		if f.Op == gateway.OpDispatch && f.T == typ {
			return f
		}
	}
}

// closeStatus waits for the connection to end and returns the close code
// the server sent (-1 when it sent none).
func (c *wsClient) closeStatus() websocket.StatusCode {
	c.t.Helper()
	timeout := time.After(wait)
	for {
		select {
		case b, ok := <-c.frames:
			if !ok {
				return websocket.CloseStatus(c.readErr())
			}
			if err := testkit.CheckGatewayFrame(b); err != nil {
				c.t.Fatalf("frame breaks the contract: %v", err)
			}
		case <-timeout:
			c.t.Fatalf("connection still open after %v", wait)
		}
	}
}

func (c *wsClient) send(op int) {
	c.t.Helper()
	c.sendRaw(websocket.MessageText, []byte(`{"op":`+itoa(op)+`,"d":null}`))
}

func (c *wsClient) sendRaw(typ websocket.MessageType, b []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := c.ws.Write(ctx, typ, b); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

// sentinel publishes a USER_UPDATE about u to u only. Everything dispatched
// to u before it arrives before it, so frames read up to it are all u will
// get from what happened earlier.
func (e *env) sentinel(u userBody) {
	e.t.Helper()
	acct, err := e.m.Auth.GetUser(e.t.Context(), u.id(e.t))
	if err != nil {
		e.t.Fatal(err)
	}
	e.bus.Publish(e.t.Context(), events.Event{Type: events.UserUpdate,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{acct.ID}},
		Payload:  acct.Public()})
}

// untilSentinel publishes a sentinel to u and returns the DISPATCH frames
// c got before it.
func (e *env) untilSentinel(c *wsClient, u userBody) []frame {
	e.t.Helper()
	e.sentinel(u)
	var got []frame
	deadline := time.Now().Add(wait)
	for {
		f, ok := c.nextWithin(time.Until(deadline))
		if !ok {
			e.t.Fatalf("sentinel not received within %v (connection error: %v)", wait, c.readErr())
		}
		if f.Op != gateway.OpDispatch {
			continue
		}
		if f.T == events.UserUpdate {
			var p userBody
			decode(e.t, f.D, &p)
			if p.ID == u.ID {
				return got
			}
		}
		got = append(got, f)
	}
}

func types(fs []frame) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.T
	}
	return out
}

// metric returns the value of a gauge or counter sample with the given
// label value (or the unlabelled sample when label is "").
func (e *env) metric(name, label string) float64 {
	e.t.Helper()
	mfs, err := e.reg.Gather()
	if err != nil {
		e.t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if label != "" && (len(m.GetLabel()) == 0 || m.GetLabel()[0].GetValue() != label) {
				continue
			}
			if g := m.GetGauge(); g != nil {
				return g.GetValue()
			}
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

// eventually polls cond until it holds or wait passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Payload shapes the tests read.
type (
	readyBody struct {
		User      userBody         `json:"user"`
		Guilds    []guildStateBody `json:"guilds"`
		Presences []presenceBody   `json:"presences"`
	}
	guildStateBody struct {
		Guild    guildBody     `json:"guild"`
		Channels []channelBody `json:"channels"`
		Members  []memberBody  `json:"members"`
		MyMember memberBody    `json:"my_member"`
	}
	memberBody struct {
		User userBody `json:"user"`
	}
	presenceBody struct {
		UserID string `json:"user_id"`
		Status string `json:"status"`
	}
)

func (r readyBody) guild(id string) (guildStateBody, bool) {
	for _, g := range r.Guilds {
		if g.Guild.ID == id {
			return g, true
		}
	}
	return guildStateBody{}, false
}

func hasChannel(cs []channelBody, id string) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (c *wsClient) readErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
