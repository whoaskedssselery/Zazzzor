package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func dialGateway(t *testing.T, c *testkit.Client) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(c.URL(), "https")+"/api/v1/gateway",
		&websocket.DialOptions{HTTPClient: c.HTTPClient(), HTTPHeader: http.Header{"Origin": {testkit.DefaultOrigin}}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	ws.SetReadLimit(1 << 20)
	return ws
}

func readOp(t *testing.T, ws *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, b, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

// The gateway route is wired, outlives the per-request deadlines and is
// logged as a 101 when the upgrade happens.
func TestGatewayRouteWired(t *testing.T) {
	// The write deadline must leave room for the password hashing of
	// the registration below.
	const deadline = 3 * time.Second
	h, logs := newModulesRouter(t, httpx.DeadlineOptions{Read: deadline / 5, Write: deadline})
	c := testkit.NewClient(t, h)
	expect := func(resp *testkit.Response, status int) {
		t.Helper()
		if resp.Status != status {
			t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
		}
	}
	expect(c.Do(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "alice", "password": "password123"}), http.StatusCreated)

	ws := dialGateway(t, c)
	if f := readOp(t, ws); !strings.HasPrefix(f, `{"op":10,`) {
		t.Fatalf("first frame %s, want HELLO", f)
	}
	if f := readOp(t, ws); !strings.Contains(f, `"t":"READY"`) {
		t.Fatalf("second frame %s, want READY", f)
	}
	time.Sleep(deadline + 300*time.Millisecond) // past both request deadlines
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"op":1,"d":null}`)); err != nil {
		t.Fatalf("heartbeat after the deadlines: %v", err)
	}
	if f := readOp(t, ws); !strings.HasPrefix(f, `{"op":11,`) {
		t.Fatalf("answer %s, want HEARTBEAT_ACK", f)
	}
	if l := logs.String(); !strings.Contains(l, `"route":"/api/v1/gateway","status":101`) {
		t.Fatalf("no 101 request log for the gateway: %s", l)
	}
}

// Shutdown closes hijacked gateway connections with 1012 and does not hang
// on them.
func TestServeClosesGatewayOnShutdown(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedOrigins = []string{testkit.DefaultOrigin}
	cfg.CookieSecure = false // plain HTTP below
	cfg.DataDir = t.TempDir()
	apiLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	metricsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	pool := testkit.Postgres(t)
	testkit.OpenRegistration(t, pool)
	go func() { done <- serve(ctx, slog.New(slog.DiscardHandler), cfg, pool, apiLn, metricsLn) }()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	hc := &http.Client{Jar: jar}
	base := "http://" + apiLn.Addr().String()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/api/v1/auth/register",
		strings.NewReader(`{"username":"alice","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testkit.DefaultOrigin)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d", resp.StatusCode)
	}

	dctx, dcancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer dcancel()
	ws, dresp, err := websocket.Dial(dctx, "ws://"+apiLn.Addr().String()+"/api/v1/gateway",
		&websocket.DialOptions{HTTPClient: hc, HTTPHeader: http.Header{"Origin": {testkit.DefaultOrigin}}})
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.CloseNow() }()
	closed := make(chan websocket.StatusCode, 1)
	go func() {
		for {
			if _, _, err := ws.Read(context.Background()); err != nil {
				closed <- websocket.CloseStatus(err)
				return
			}
		}
	}()

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(gatewayShutdownTimeout + 5*time.Second):
		t.Fatal("serve did not stop with a gateway connection open")
	}
	if d := time.Since(start); d > gatewayShutdownTimeout {
		t.Errorf("shutdown took %v", d)
	}
	select {
	case code := <-closed:
		if code != gateway.CloseServiceRestart {
			t.Fatalf("close code = %d, want 1012", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was not closed")
	}
}
