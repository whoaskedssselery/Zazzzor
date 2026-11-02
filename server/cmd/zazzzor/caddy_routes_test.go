package main_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// caddyHandle copies a production handle verbatim, including nested directives.
func caddyHandle(t *testing.T, source, path string) string {
	t.Helper()
	start := strings.Index(source, "handle "+path+" {")
	if start < 0 {
		t.Fatalf("production Caddy handle %s missing", path)
	}
	depth := 0
	for offset, char := range source[start:] {
		switch char {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start : start+offset+1]
			}
		}
	}
	t.Fatalf("unclosed production Caddy handle %s", path)
	return ""
}

func TestCaddyKeepsLiveKitWebhookInternal(t *testing.T) {
	const deploy = "../../../deploy"
	source, err := os.ReadFile(deploy + "/caddy/Caddyfile")
	if err != nil {
		t.Fatalf("read production Caddyfile: %v", err)
	}
	pins, err := os.ReadFile(deploy + "/.env.ci")
	if err != nil {
		t.Fatalf("read pinned Caddy version: %v", err)
	}
	version := ""
	for _, line := range strings.Split(string(pins), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "CADDY_VERSION="); ok {
			version = value
		}
	}
	if version == "" {
		t.Fatal("CADDY_VERSION missing from CI configuration")
	}
	// Only ACME/domain setup is replaced. API/signaling handles are copied
	// unchanged, and both upstreams are local deterministic Caddy listeners.
	config := fmt.Sprintf(`{
 admin off
 auto_https off
}
:8080 {
 %s
 %s
 handle {
  respond 404
 }
}
:9090 {
 respond "api upstream" 200
}
:7880 {
 respond "media upstream" 200
}
`, caddyHandle(t, string(source), "/api/*"), caddyHandle(t, string(source), "/rtc*"))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "caddy:" + version,
			ExposedPorts: []string{"8080/tcp"},
			Env:          map[string]string{"ZAZZZOR_API_PORT": "9090"},
			Labels:       map[string]string{testkit.Label: "1"},
			Files:        []testcontainers.ContainerFile{{Reader: strings.NewReader(config), ContainerFilePath: "/etc/caddy/Caddyfile", FileMode: 0o644}},
			WaitingFor:   wait.ForListeningPort("8080/tcp"),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start Caddy routing test: %v", err)
	}
	base, err := container.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatalf("Caddy endpoint: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{http.MethodPost, "/api/v1/livekit/webhook", 404, ""},
		{http.MethodGet, "/api/v1/livekit/webhook?probe=1", 404, ""},
		{http.MethodPost, "/api/v1/livekit/%77ebhook", 404, ""},
		{http.MethodGet, "/api/v1/healthz", 200, "api upstream"},
		{http.MethodGet, "/api/v1/livekit/webhook-other", 200, "api upstream"},
		{http.MethodGet, "/rtc", 200, "media upstream"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), tc.method, base+tc.path, strings.NewReader("untrusted body"))
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Caddy route request: %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatalf("Caddy route response: %v", err)
			}
			if resp.StatusCode != tc.status || string(body) != tc.body {
				t.Fatalf("Caddy response=%d/%q, want %d/%q", resp.StatusCode, body, tc.status, tc.body)
			}
		})
	}
}
