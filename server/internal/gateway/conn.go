package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// conn is one gateway connection. Its run goroutine is the only writer;
// readLoop is the only reader. The dispatcher hands it frames through send
// and never waits for it.
type conn struct {
	g         *Gateway
	ws        *websocket.Conn
	user      auth.User
	sessionID snowflake.ID
	// token is the session cookie, checked once more after registration
	// and then forgotten.
	token string

	ctx    context.Context // the reader's; cancelled when run ends
	cancel context.CancelFunc
	// writeCtx bounds every write; cancelled to abort a write stuck on a
	// peer that stopped reading (4008).
	writeCtx    context.Context
	writeCancel context.CancelFunc

	send     chan outgoing
	beat     chan struct{} // a heartbeat arrived
	peerGone chan struct{} // closed by readLoop when reading failed

	closeOnce sync.Once
	closing   atomic.Bool
	closeCode websocket.StatusCode // written before closeReq is closed
	closeReq  chan struct{}
	// abort skips the close handshake (a peer that stopped reading).
	abort atomic.Bool

	seq int64 // run only
	// online: counted in presence (guarded by Gateway.mu).
	online bool
}

func newConn(g *Gateway, ws *websocket.Conn, user auth.User, sessionID snowflake.ID, token string) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	writeCtx, writeCancel := context.WithCancel(context.Background())
	return &conn{g: g, ws: ws, user: user, sessionID: sessionID, token: token, ctx: ctx, cancel: cancel,
		writeCtx: writeCtx, writeCancel: writeCancel,
		send: make(chan outgoing, g.opts.SendBuffer), beat: make(chan struct{}, 1),
		peerGone: make(chan struct{}), closeReq: make(chan struct{})}
}

// closeWith asks the connection to close with code; only the first call
// counts. It never blocks.
func (c *conn) closeWith(code websocket.StatusCode) {
	c.closeOnce.Do(func() {
		c.closeCode = code
		c.closing.Store(true)
		close(c.closeReq)
	})
}

// enqueue queues a frame without blocking; a full buffer closes the
// connection with 4008 overloaded.
func (c *conn) enqueue(o outgoing) {
	if c.closing.Load() {
		return
	}
	select {
	case c.send <- o:
	default:
		// The peer is not reading: a close frame would not get through
		// either, so drop the connection without the handshake and do
		// not wait for a write stuck on it.
		c.abort.Store(true)
		c.closeWith(CloseOverloaded)
		c.writeCancel()
	}
}

func (c *conn) run() {
	defer c.g.unregister(c)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		c.readLoop()
	}()
	c.serve()
	c.cancel()
	c.writeCancel()
	_ = c.ws.CloseNow()
	<-readerDone
	label := "peer"
	if c.closing.Load() {
		label = strconv.Itoa(int(c.closeCode))
	}
	c.g.m.closes.WithLabelValues(label).Inc()
}

// serve writes HELLO and READY, then frames, heartbeat acks and finally the
// close frame, until a close was requested or the peer is gone.
func (c *conn) serve() {
	hb := time.NewTimer(c.g.opts.HeartbeatTimeout)
	defer hb.Stop()

	hello, _ := json.Marshal(Hello{HeartbeatIntervalMS: HeartbeatInterval.Milliseconds()})
	if !c.write(outgoing{op: OpHello, d: hello}) {
		return
	}
	if !c.sendReady() {
		return
	}
	for {
		// A requested close wins over frames that are still queued.
		select {
		case <-c.closeReq:
			c.closeNow()
			return
		default:
		}
		select {
		case <-c.closeReq:
			c.closeNow()
			return
		case <-c.peerGone:
			return
		case <-hb.C:
			c.closeWith(CloseSessionTimeout)
		case <-c.beat:
			hb.Reset(c.g.opts.HeartbeatTimeout)
			if !c.write(outgoing{op: OpHeartbeatAck, d: nullJSON}) {
				return
			}
		case o := <-c.send:
			if !c.write(o) {
				return
			}
		}
	}
}

func (c *conn) closeNow() {
	if c.abort.Load() {
		_ = c.ws.CloseNow()
		return
	}
	_ = c.ws.Close(c.closeCode, closeReasons[c.closeCode])
}

// sendReady re-checks the session, then builds and writes READY. A revoked
// session or a failed build requests a close and still returns true (the
// loop then closes); false means the write failed.
func (c *conn) sendReady() bool {
	ctx, cancel := context.WithTimeout(c.g.ctx, readyTimeout)
	defer cancel()
	// The session may have been revoked between the handshake and the
	// registration, when its SESSION_REVOKED found no connection to close.
	// Revocations dispatched from now on do find this one.
	user, sessionID, err := c.g.opts.Auth.Authenticate(ctx, c.token)
	c.token = ""
	switch {
	case errors.Is(err, auth.ErrUnauthenticated) || (err == nil && sessionID != c.sessionID):
		c.closeWith(CloseAuthFailed)
		return true
	case err != nil:
		c.g.log.Error("gateway re-authenticate", "user_id", c.user.ID.String(), "err", err.Error())
		c.closeWith(closeInternal)
		return true
	}
	c.g.markOnline(c)
	ready, err := c.g.ready(ctx, user)
	if err != nil {
		c.g.log.Error("gateway build ready", "user_id", user.ID.String(), "err", err.Error())
		c.closeWith(closeInternal)
		return true
	}
	d, err := json.Marshal(ready)
	if err != nil {
		c.g.log.Error("gateway encode ready", "err", err.Error())
		c.closeWith(closeInternal)
		return true
	}
	return c.write(outgoing{op: OpDispatch, t: EventReady, d: d})
}

// write sends one frame within writeTimeout; on failure the library has
// closed the connection.
func (c *conn) write(o outgoing) bool {
	f := frame{Op: o.op, T: o.t, D: o.d}
	if o.op == OpDispatch {
		c.seq++
		f.S = c.seq
	}
	b, err := json.Marshal(f)
	if err != nil {
		c.g.log.Error("gateway encode frame", "type", o.t, "err", err.Error())
		return false
	}
	ctx, cancel := context.WithTimeout(c.writeCtx, writeTimeout)
	defer cancel()
	if err = c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		return false
	}
	if o.op == OpDispatch {
		c.g.m.events.WithLabelValues(o.t).Inc()
	}
	return true
}

// readLoop reads client frames: only text frames of at most MaxFrameBytes
// holding {"op": 1, ...} are valid; anything else closes with 4002.
func (c *conn) readLoop() {
	for {
		typ, r, err := c.ws.Reader(c.ctx)
		if err != nil {
			close(c.peerGone)
			return
		}
		if typ != websocket.MessageText {
			c.closeWith(CloseDecodeError)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r, MaxFrameBytes+1))
		if err != nil {
			close(c.peerGone)
			return
		}
		var f clientFrame
		if len(b) > MaxFrameBytes || json.Unmarshal(b, &f) != nil || f.Op == nil || *f.Op != OpHeartbeat {
			c.closeWith(CloseDecodeError)
			return
		}
		select {
		case c.beat <- struct{}{}:
		default: // one is pending already
		}
	}
}
