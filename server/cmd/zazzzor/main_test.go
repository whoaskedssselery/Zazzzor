package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

func testConfig() config.Config {
	cfg, err := config.Load(func(k string) string {
		return map[string]string{
			"ZAZZZOR_DATABASE_URL":    "postgres://test",
			"ZAZZZOR_ALLOWED_ORIGINS": "https://zazzzor.example",
		}[k]
	})
	if err != nil {
		panic(err)
	}
	return cfg
}

func newTestRouter(t *testing.T) (http.Handler, *prometheus.Registry, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	reg := prometheus.NewRegistry()
	h := newRouter(routerDeps{
		Config:  testConfig(),
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
		Metrics: newHTTPMetrics(reg),
	})
	return h, reg, &logs
}

func TestHealthAndReady(t *testing.T) {
	h, _, _ := newTestRouter(t)
	for _, path := range []string{"/api/v1/healthz", "/api/v1/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: Content-Type = %q", path, ct)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
			t.Errorf("%s: body = %s", path, rec.Body.String())
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Errorf("%s: no X-Request-Id", path)
		}
	}
}

func TestUnknownRouteIsProblemJSON(t *testing.T) {
	h, _, _ := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/healthz", nil))
	// Origin check runs before routing, so a foreign-origin mutation is refused first.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE without Origin: status = %d, want 403", rec.Code)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/healthz", nil)
	req.Header.Set("Origin", "https://zazzzor.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE healthz: status = %d, want 405", rec.Code)
	}
}

func TestPanicIsRecoveredAsInternal(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := withRecover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom secret")
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(httpx.WithLogger(req.Context(), logger))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("body leaks panic value: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "panic") {
		t.Errorf("panic not logged: %q", logs.String())
	}
}

func TestRequestMetricsUseRoutePattern(t *testing.T) {
	h, reg, logs := newTestRouter(t)
	for range 3 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/random-123", nil))

	rec := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	out := rec.Body.String()
	for _, want := range []string{
		`zazzzor_http_requests_total{method="GET",route="/api/v1/healthz",status="200"} 3`,
		`zazzzor_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`zazzzor_http_request_duration_seconds_count{method="GET",route="/api/v1/healthz"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "random-123") {
		t.Error("raw path leaked into metric labels")
	}
	if !strings.Contains(logs.String(), `"route":"/api/v1/healthz"`) {
		t.Errorf("request log lacks route: %s", logs.String())
	}
}

func TestHealthcheckCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	env := func(addr string) func(string) string {
		return func(k string) string {
			if k == "ZAZZZOR_HTTP_ADDR" {
				return addr
			}
			return ""
		}
	}
	if code := run([]string{"healthcheck"}, env(":"+port), io.Discard, io.Discard); code != 0 {
		t.Errorf("healthcheck against healthy server = %d, want 0", code)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, deadPort, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	if code := run([]string{"healthcheck"}, env(":"+deadPort), io.Discard, io.Discard); code != 1 {
		t.Errorf("healthcheck against closed port = %d, want 1", code)
	}
}

func TestVersionCommandNeedsNoConfig(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--version"}, func(string) string { return "" }, &out, io.Discard); code != 0 {
		t.Fatalf("--version exit = %d", code)
	}
	if strings.TrimSpace(out.String()) != version {
		t.Errorf("--version printed %q, want %q", out.String(), version)
	}
}

func TestServeRequiresConfig(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, func(string) string { return "" }, io.Discard, &stderr); code != 1 {
		t.Fatalf("serve with empty env exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "ZAZZZOR_DATABASE_URL") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestServeShutsDownGracefully(t *testing.T) {
	apiLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	metricsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, slog.New(slog.DiscardHandler), testConfig(), nil, apiLn, metricsLn)
	}()

	client := &http.Client{Timeout: 3 * time.Second}
	for _, u := range []string{
		"http://" + apiLn.Addr().String() + "/api/v1/healthz",
		"http://" + metricsLn.Addr().String() + "/metrics",
	} {
		resp, err := client.Get(u)
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d", u, resp.StatusCode)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after context cancel")
	}
}
