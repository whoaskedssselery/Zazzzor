package gateway_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// upgradeHeaders make a plain request look like a WebSocket handshake, so
// the refusals before the upgrade go through testkit's contract check.
var upgradeHeaders = []testkit.RequestOption{
	testkit.WithHeader("Connection", "Upgrade"), testkit.WithHeader("Upgrade", "websocket"),
	testkit.WithHeader("Sec-WebSocket-Version", "13"), testkit.WithHeader("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ=="),
}

// refusal sends a handshake that must be refused before the upgrade.
func refusal(t *testing.T, c *testkit.Client, status int, code string) *testkit.Response {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/gateway", nil, upgradeHeaders...)
	var p struct{ Code string }
	resp.JSON(&p)
	if resp.Status != status || p.Code != code {
		t.Fatalf("handshake = %d %s, want %d %s", resp.Status, resp.Body, status, code)
	}
	return resp
}

func oneEvery(d time.Duration, burst int) *ratelimit.Limiter {
	return ratelimit.New(rate.Every(d), burst, time.Hour)
}

func TestGatewayConnectionsPerUserCapped(t *testing.T) {
	e := newEnv(t, gateway.Options{MaxConnsPerUser: 2})
	alice, _ := e.user("alice")
	bob, _ := e.user("bob")
	first, _ := connect(t, alice)
	connect(t, alice)
	refusal(t, alice, http.StatusTooManyRequests, "too_many_connections")
	if n := e.metric("zazzzor_gateway_connections", ""); n != 2 {
		t.Fatalf("connections = %v after the refusal, want 2", n)
	}
	// Another user is not affected.
	connect(t, bob)

	_ = first.ws.Close(websocket.StatusNormalClosure, "")
	eventually(t, "the first connection to go", func() bool { return e.metric("zazzzor_gateway_connections", "") == 2 })
	connect(t, alice)
}

func TestGatewayConnectionsCappedInAll(t *testing.T) {
	e := newEnv(t, gateway.Options{MaxConns: 1})
	alice, _ := e.user("alice")
	bob, _ := e.user("bob")
	connect(t, alice)
	resp := refusal(t, bob, http.StatusServiceUnavailable, "server_busy")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("503 without Retry-After")
	}
}

func TestGatewayUpgradesRateLimitedPerUser(t *testing.T) {
	e := newEnv(t, gateway.Options{UserUpgradeLimiter: oneEvery(time.Hour, 2)})
	alice, _ := e.user("alice")
	bob, _ := e.user("bob")
	connect(t, alice)
	connect(t, alice)
	resp := refusal(t, alice, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	connect(t, bob)
}

// TestGatewayUpgradesRateLimitedPerIP: the per-address limit applies before
// the session is looked at, so it also stops attempts without one.
func TestGatewayUpgradesRateLimitedPerIP(t *testing.T) {
	e := newEnv(t, gateway.Options{IPUpgradeLimiter: oneEvery(time.Hour, 2)})
	alice, _ := e.user("alice")
	anon := testkit.NewClient(t, e.h)
	refusal(t, anon, http.StatusUnauthorized, "unauthenticated")
	connect(t, alice)
	resp := refusal(t, anon, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	refusal(t, alice, http.StatusTooManyRequests, "rate_limited")
	// A foreign Origin is still refused first.
	resp = alice.Do(http.MethodGet, "/api/v1/gateway", nil, append(upgradeHeaders, testkit.WithOrigin("https://evil.example"))...)
	if resp.Status != http.StatusForbidden {
		t.Fatalf("foreign origin = %d %s, want 403", resp.Status, resp.Body)
	}
}
