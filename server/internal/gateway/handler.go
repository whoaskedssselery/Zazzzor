package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Authenticator resolves a session cookie (auth.Service).
type Authenticator interface {
	Authenticate(ctx context.Context, rawToken string) (auth.User, snowflake.ID, error)
}

// Bus is the event bus the gateway subscribes to and announces presence on
// (events.Bus).
type Bus interface {
	Subscribe(buffer int) (<-chan events.Event, func())
	events.Publisher
}

var _ Bus = (*events.Bus)(nil)

// Options configures a Gateway. Auth, Guilds, Channels and Bus are
// required; zero tuning fields take the defaults.
type Options struct {
	Auth     Authenticator
	Guilds   guilds.Directory
	Channels channels.Resolver
	Bus      Bus
	// AllowedOrigins are the only Origin values accepted for the upgrade.
	AllowedOrigins []string
	// Logger is for failures; nil discards.
	Logger *slog.Logger
	// Registerer exports the gateway metrics; nil keeps them unexported.
	Registerer prometheus.Registerer

	// HeartbeatTimeout closes a silent connection (4009); default 45 s.
	HeartbeatTimeout time.Duration
	// PresenceGrace delays offline after the last connection; default 5 s.
	PresenceGrace time.Duration
	// SendBuffer is the per-connection frame buffer; default 256.
	SendBuffer int
	// BusBuffer is the dispatcher's subscription buffer; default 1024.
	BusBuffer int
}

// Gateway serves /gateway and dispatches events. Create it with New and
// stop it with Close.
type Gateway struct {
	opts   Options
	log    *slog.Logger
	m      metrics
	ctx    context.Context // cancelled by Close; bounds dispatch queries
	cancel context.CancelFunc

	mu       sync.Mutex
	closed   bool
	conns    map[*conn]struct{}
	unsub    func()
	presence presence
	connWG   sync.WaitGroup

	dispatchDone chan struct{}
}

type metrics struct {
	connections    prometheus.Gauge
	events         *prometheus.CounterVec
	closes         *prometheus.CounterVec
	dispatchErrors prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) metrics {
	m := metrics{
		connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "zazzzor_gateway_connections",
			Help: "Open gateway connections.",
		}),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zazzzor_gateway_events_total",
			Help: "Event frames queued to gateway connections, by type.",
		}, []string{"type"}),
		closes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zazzzor_gateway_closes_total",
			Help: "Closed gateway connections, by the close code the server sent (peer: closed by the client or the network).",
		}, []string{"code"}),
		dispatchErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "zazzzor_gateway_dispatch_errors_total",
			Help: "Events (or per-recipient payloads) dropped because their recipients could not be resolved.",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.connections, m.events, m.closes, m.dispatchErrors)
	}
	return m
}

// New returns a running gateway: it is subscribed to the bus before it
// returns, so no event published afterwards is missed.
func New(o Options) *Gateway {
	if o.HeartbeatTimeout <= 0 {
		o.HeartbeatTimeout = DefaultHeartbeatTimeout
	}
	if o.PresenceGrace <= 0 {
		o.PresenceGrace = DefaultPresenceGrace
	}
	if o.SendBuffer <= 0 {
		o.SendBuffer = DefaultSendBuffer
	}
	if o.BusBuffer <= 0 {
		o.BusBuffer = DefaultBusBuffer
	}
	o.AllowedOrigins = slices.Clone(o.AllowedOrigins)
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{opts: o, log: log, m: newMetrics(o.Registerer), ctx: ctx, cancel: cancel,
		conns: make(map[*conn]struct{}), presence: newPresence(), dispatchDone: make(chan struct{})}
	ch, unsub := o.Bus.Subscribe(o.BusBuffer)
	g.unsub = unsub
	go g.dispatchLoop(ch)
	return g
}

// ServeHTTP checks the Origin (403) and the session cookie (401) before it
// upgrades; both refusals are problem+json responses. It returns as soon as
// the connection is upgraded (the request is logged as 101); the connection
// lives on in its own goroutines. The route must be wrapped in
// httpx.WithoutDeadlines: the gateway bounds every read and write itself.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !httpx.OriginAllowed(g.opts.AllowedOrigins, r) {
		httpx.WriteProblem(w, httpx.Forbidden("origin_not_allowed"))
		return
	}
	ck, err := r.Cookie(auth.CookieName)
	if err != nil || ck.Value == "" {
		httpx.WriteProblem(w, httpx.Unauthorized("unauthenticated"))
		return
	}
	ctx := r.Context()
	user, sessionID, err := g.opts.Auth.Authenticate(ctx, ck.Value)
	if errors.Is(err, auth.ErrUnauthenticated) {
		httpx.WriteProblem(w, httpx.Unauthorized("unauthenticated"))
		return
	}
	if err != nil {
		httpx.LoggerFrom(ctx).ErrorContext(ctx, "gateway authenticate", "err", err.Error())
		httpx.WriteProblem(w, httpx.Internal())
		return
	}
	if !httpx.IsWebSocketUpgrade(r) {
		httpx.WriteProblem(w, &httpx.Problem{Status: http.StatusUpgradeRequired, Code: "upgrade_required",
			Title: http.StatusText(http.StatusUpgradeRequired)})
		return
	}
	// The Origin was checked above against the exact allowed origins.
	// coder/websocket's own check (OriginPatterns) would also admit an
	// Origin whose host equals the request's Host header and ignores the
	// scheme unless a pattern names it, so it is weaker; it is skipped
	// rather than relied on.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept has answered the request (a malformed handshake)
	}
	// One byte more than allowed is read, so an oversized frame is closed
	// by readLoop with 4002 instead of by the library with 1009.
	ws.SetReadLimit(MaxFrameBytes + 1)

	c := newConn(g, ws, user, sessionID, ck.Value)
	if !g.register(c) {
		go func() { _ = ws.Close(CloseServiceRestart, closeReasons[CloseServiceRestart]) }()
		return
	}
	go c.run() //nolint:contextcheck // the connection outlives the request; it has contexts of its own
}

// register adds c to the live connections; false once
// the gateway is closed.
func (g *Gateway) register(c *conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.conns[c] = struct{}{}
	g.connWG.Add(1)
	g.m.connections.Inc()
	return true
}

// markOnline counts c in presence once its session was re-checked after
// registration; a connection refused there never shows the user online.
func (g *Gateway) markOnline(c *conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	c.online = true
	g.presenceConnected(c.user.ID)
}

// unregister removes c; called once, when its goroutines are done.
func (g *Gateway) unregister(c *conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.conns, c)
	g.m.connections.Dec()
	if c.online {
		g.presenceDisconnected(c.user.ID)
	}
	g.connWG.Done()
}

// connsByUser snapshots the live connections.
func (g *Gateway) connsByUser() map[snowflake.ID][]*conn {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[snowflake.ID][]*conn, len(g.conns))
	for c := range g.conns {
		out[c.user.ID] = append(out[c.user.ID], c)
	}
	return out
}

// closeAll asks every live connection to close with code.
func (g *Gateway) closeAll(code websocket.StatusCode) {
	g.mu.Lock()
	cs := make([]*conn, 0, len(g.conns))
	for c := range g.conns {
		cs = append(cs, c)
	}
	g.mu.Unlock()
	for _, c := range cs {
		c.closeWith(code)
	}
}

// Close stops the gateway: it stops dispatching, closes every connection
// with 1012 service_restart and waits for them until ctx is done (it then
// returns ctx.Err() and leaves the rest to the process exit). New upgrades
// are closed with 1012 at once. Close is idempotent.
func (g *Gateway) Close(ctx context.Context) error {
	g.mu.Lock()
	if !g.closed {
		g.closed = true
		g.presence.stop()
	}
	unsub := g.unsub
	g.mu.Unlock()

	// Cancel first: queries in flight then fail as a shutdown, which the
	// dispatcher does not count as a dispatch error.
	g.cancel()
	unsub() // the dispatcher sees its channel closed and returns
	g.closeAll(CloseServiceRestart)

	done := make(chan struct{})
	go func() {
		g.connWG.Wait()
		<-g.dispatchDone
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
