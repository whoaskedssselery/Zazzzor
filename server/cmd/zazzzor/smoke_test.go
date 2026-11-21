package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

const smokeTestOrigin = "https://zazzzor.example"

func smokeEnv(origins string) func(string) string {
	return func(k string) string {
		if k == "ZAZZZOR_ALLOWED_ORIGINS" {
			return origins
		}
		return ""
	}
}

// gatewayMode is what the fake gateway does after the upgrade.
type gatewayMode int

const (
	gatewayReady   gatewayMode = iota // HELLO, then READY
	gatewayMissing                    // 404, no upgrade
	gatewayNoReady                    // HELLO, then silence
)

// fakeAPI is just enough of the API for smoke: it checks Origin on every
// write and the session cookie on everything after registration.
type fakeAPI struct {
	t        *testing.T
	gateway  gatewayMode
	register int    // status to answer registration with; 0 means 201
	origin   string // Origin the gateway expects; empty means smokeTestOrigin

	// closed, when set, gets the close code the gateway read, once its
	// handler has it (the client may return before that).
	closed chan websocket.StatusCode

	mu         sync.Mutex
	deleted    bool
	gatewayHit bool
}

const (
	fakeCookie  = "s3cret-token"
	fakeGuild   = "100"
	fakeChannel = "101"
	fakeMessage = "102"
)

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Header.Get("Origin") != smokeTestOrigin {
		f.t.Errorf("%s %s: Origin = %q", r.Method, r.URL.Path, r.Header.Get("Origin"))
		problem(w, http.StatusForbidden, "origin_not_allowed")
		return
	}
	if r.URL.Path == "/api/v1/auth/register" {
		if f.register != 0 {
			problem(w, f.register, "rate_limited")
			return
		}
		var body struct{ Username, Password string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || len(body.Password) < 8 {
			problem(w, http.StatusBadRequest, "validation_failed")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "zz_session", Value: fakeCookie, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		writeJSON(w, http.StatusCreated, map[string]string{"id": "1", "username": body.Username})
		return
	}
	if c, err := r.Cookie("zz_session"); err != nil || c.Value != fakeCookie {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/v1/guilds":
		writeJSON(w, http.StatusCreated, map[string]string{"id": fakeGuild, "name": "smoke"})
	case "GET /api/v1/guilds/" + fakeGuild + "/channels":
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "99", "type": 4, "name": "cat"},
			{"id": fakeChannel, "type": 0, "name": "general"},
		})
	case "POST /api/v1/channels/" + fakeChannel + "/messages":
		writeJSON(w, http.StatusCreated, map[string]string{"id": fakeMessage})
	case "GET /api/v1/channels/" + fakeChannel + "/messages":
		writeJSON(w, http.StatusOK, []map[string]string{{"id": fakeMessage}})
	case "DELETE /api/v1/guilds/" + fakeGuild:
		f.mu.Lock()
		f.deleted = true
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "POST /api/v1/auth/logout":
		w.WriteHeader(http.StatusNoContent)
	case "GET /api/v1/gateway":
		f.serveGateway(w, r)
	default:
		problem(w, http.StatusNotFound, "not_found")
	}
}

func (f *fakeAPI) serveGateway(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.gatewayHit = true
	f.mu.Unlock()
	if f.gateway == gatewayMissing {
		problem(w, http.StatusNotFound, "not_found")
		return
	}
	want := f.origin
	if want == "" {
		want = smokeTestOrigin
	}
	if got := r.Header.Get("Origin"); got != want {
		f.t.Errorf("gateway Origin = %q, want %q", got, want)
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		f.t.Errorf("accept: %v", err)
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx := r.Context()
	if err = c.Write(ctx, websocket.MessageText, []byte(`{"op":10,"d":{"heartbeat_interval_ms":30000}}`)); err != nil {
		return
	}
	if f.gateway == gatewayReady {
		if err = c.Write(ctx, websocket.MessageText, []byte(`{"op":0,"t":"READY","s":1,"d":{"user":{"id":"1"},"guilds":[]}}`)); err != nil {
			return
		}
	}
	_, _, err = c.Read(ctx)
	if f.closed != nil {
		f.closed <- websocket.CloseStatus(err)
	}
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "code": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func runSmokeAgainst(t *testing.T, h http.Handler, opts smokeOptions) (code int, stdout, stderr string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	code = runSmoke(t.Context(), srv.URL, smokeTestOrigin, opts, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestSmokeSucceeds(t *testing.T) {
	f := &fakeAPI{t: t, closed: make(chan websocket.StatusCode, 1)}
	code, out, errOut := runSmokeAgainst(t, f, smokeOptions{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); lines[len(lines)-1] != "OK" {
		t.Errorf("last line of stdout = %q, want OK; stdout:\n%s", lines[len(lines)-1], out)
	}
	if strings.Contains(out+errOut, fakeCookie) {
		t.Error("the session token leaked into the output")
	}
	f.mu.Lock()
	deleted := f.deleted
	f.mu.Unlock()
	if !deleted {
		t.Error("the smoke guild was not deleted")
	}
	select {
	case code := <-f.closed:
		if code != websocket.StatusNormalClosure {
			t.Errorf("gateway close code = %v, want 1000", code)
		}
	case <-time.After(5 * time.Second):
		t.Error("the gateway never saw the connection close")
	}
}

func TestSmokeReportsMissingGateway(t *testing.T) {
	f := &fakeAPI{t: t, gateway: gatewayMissing}
	code, _, errOut := runSmokeAgainst(t, f, smokeOptions{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{"WebSocket", "/api/v1/gateway", "404"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q does not mention %q", errOut, want)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deleted {
		t.Error("the smoke guild was not deleted after the failure")
	}
}

func TestSmokeWaitsForReadyWithTimeout(t *testing.T) {
	f := &fakeAPI{t: t, gateway: gatewayNoReady}
	start := time.Now()
	code, _, errOut := runSmokeAgainst(t, f, smokeOptions{GatewayTimeout: 300 * time.Millisecond})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "READY") {
		t.Errorf("stderr %q does not mention READY", errOut)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("smoke took %v, want about the 300ms gateway timeout", d)
	}
}

func TestSmokeExplainsRegistrationLimit(t *testing.T) {
	f := &fakeAPI{t: t, register: http.StatusTooManyRequests}
	code, _, errOut := runSmokeAgainst(t, f, smokeOptions{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{"sign-up", "429", "3 sign-ups an hour"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q does not mention %q", errOut, want)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gatewayHit {
		t.Error("smoke went on after a failed step")
	}
}

func TestSmokeCommandUsage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		origins string
		want    string
	}{
		{"no url", []string{"smoke"}, smokeTestOrigin, "zazzzor smoke"},
		{"bad url", []string{"smoke", "ftp://x"}, smokeTestOrigin, "http"},
		{"no origins", []string{"smoke", "http://127.0.0.1:1"}, "", "ZAZZZOR_ALLOWED_ORIGINS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			if code := run(tc.args, smokeEnv(tc.origins), &bytes.Buffer{}, &errOut); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr %q does not mention %q", errOut.String(), tc.want)
			}
		})
	}
}

func TestSmokeCommandReportsUnreachableAPI(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"smoke", "http://127.0.0.1:1"}, smokeEnv(smokeTestOrigin), &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "sign-up") {
		t.Errorf("stderr %q does not name the failed step", errOut.String())
	}
}

// TestSmokeAgainstRealAPI runs the REST steps against the production router
// on a real database; only the gateway is faked, so this test keeps working
// when the real one is mounted.
func TestSmokeAgainstRealAPI(t *testing.T) {
	api, _ := newAuthRouter(t)
	fake := &fakeAPI{t: t, origin: testkit.DefaultOrigin}
	mux := http.NewServeMux()
	mux.Handle("/", api)
	mux.Handle("/api/v1/gateway", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The real router issued a real token; the fake gateway only needs
		// to see that one is sent.
		if c, err := r.Cookie("zz_session"); err != nil || c.Value == "" {
			problem(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		fake.serveGateway(w, r)
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	if code := runSmoke(t.Context(), srv.URL, testkit.DefaultOrigin, smokeOptions{}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errOut.String())
	}
}
