package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// TestSmokeVoiceWithLiveKit exercises the command's real token, peer and
// signed webhook path. Only the chosen failure is intercepted.
func TestSmokeVoiceWithLiveKit(t *testing.T) {
	const key = "smokekey"
	const secret = "0123456789abcdef0123456789abcdef"
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	listenHost, callbackHost := "127.0.0.1", testcontainers.HostInternal
	if _, err := os.Stat("/.dockerenv"); err == nil {
		hostname, hostErr := os.Hostname()
		if hostErr != nil {
			t.Fatalf("read runner hostname: %v", hostErr)
		}
		ips, lookupErr := net.DefaultResolver.LookupIP(ctx, "ip4", hostname)
		if lookupErr != nil || len(ips) == 0 {
			t.Fatalf("resolve runner callback address: %v", lookupErr)
		}
		listenHost, callbackHost = ips[0].String(), ips[0].String()
	} else if !os.IsNotExist(err) {
		t.Fatalf("detect containerized runner: %v", err)
	}
	var active atomic.Value
	var dropped atomic.Value
	dropped.Store("")
	var proxy atomic.Pointer[httputil.ReverseProxy]
	var guildCreated, guildDeleted, voiceCleaned atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rtc") {
			proxy.Load().ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/livekit/webhook" {
			data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			_ = r.Body.Close()
			if err != nil {
				http.Error(w, "read webhook", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			var event struct{ Event string }
			if json.Unmarshal(data, &event) == nil && event.Event == dropped.Load().(string) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if handler := active.Load(); handler != nil {
			rec := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			handler.(http.Handler).ServeHTTP(rec, r)
			if rec.Status() >= 200 && rec.Status() < 300 {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/guilds":
					guildCreated.Store(true)
				case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/guilds/"):
					guildDeleted.Store(true)
				case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/users/@me/voice":
					voiceCleaned.Store(true)
				}
			}
		} else {
			http.Error(w, "initializing", http.StatusServiceUnavailable)
		}
	}))
	_ = srv.Listener.Close()
	listener, err := net.Listen("tcp", net.JoinHostPort(listenHost, "0"))
	if err != nil {
		t.Fatalf("listen for smoke callbacks: %v", err)
	}
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	_, callbackPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("read callback port: %v", err)
	}
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve RTC TCP port: %v", err)
	}
	mediaPort := port.Addr().(*net.TCPAddr).Port
	_ = port.Close()
	nodeIP := "127.0.0.1"
	if host := os.Getenv("TESTCONTAINERS_HOST_OVERRIDE"); host != "" {
		ips, lookupErr := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if lookupErr != nil || len(ips) == 0 {
			t.Fatalf("resolve Docker RTC address: %v", lookupErr)
		}
		nodeIP = ips[0].String()
	}
	pins, err := os.ReadFile("../../../deploy/.env.ci")
	if err != nil {
		t.Fatalf("read LiveKit pin: %v", err)
	}
	var version string
	for _, line := range strings.Split(string(pins), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "LIVEKIT_VERSION="); ok {
			version = value
		}
	}
	if version == "" {
		t.Fatal("LIVEKIT_VERSION is missing from CI pins")
	}
	livekitConfig := fmt.Sprintf(`port: 7880
rtc:
  tcp_port: %d
  use_external_ip: false
  node_ip: %s
keys:
  %s: %s
webhook:
  api_key: %s
  urls:
    - http://%s:%s/api/v1/livekit/webhook
logging:
  level: error
`, mediaPort, nodeIP, key, secret, key, callbackHost, callbackPort)
	rtcPort, err := network.ParsePort(fmt.Sprintf("%d/tcp", mediaPort))
	if err != nil {
		t.Fatalf("parse RTC port: %v", err)
	}
	request := testcontainers.ContainerRequest{
		Image:        "livekit/livekit-server:" + version,
		Env:          map[string]string{"LIVEKIT_CONFIG": livekitConfig},
		ExposedPorts: []string{"7880/tcp", fmt.Sprintf("%d/tcp", mediaPort)},
		HostConfigModifier: func(h *container.HostConfig) {
			if h.PortBindings == nil {
				h.PortBindings = network.PortMap{}
			}
			h.PortBindings[rtcPort] = []network.PortBinding{{HostPort: fmt.Sprint(mediaPort)}}
		},
		Labels:     map[string]string{testkit.Label: "1"},
		WaitingFor: tcwait.ForHTTP("/").WithPort("7880/tcp"),
	}
	if listenHost == "127.0.0.1" {
		var p int
		if _, scanErr := fmt.Sscan(callbackPort, &p); scanErr != nil {
			t.Fatalf("parse callback port: %v", scanErr)
		}
		request.HostAccessPorts = []int{p}
	}
	sfu, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: true})
	testcontainers.CleanupContainer(t, sfu)
	if err != nil {
		t.Fatalf("start smoke LiveKit: %v", err)
	}
	endpoint, err := sfu.PortEndpoint(ctx, "7880/tcp", "http")
	if err != nil {
		t.Fatalf("read LiveKit endpoint: %v", err)
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse LiveKit endpoint: %v", err)
	}
	proxy.Store(httputil.NewSingleHostReverseProxy(target))
	for _, tc := range []struct {
		name, drop, want string
	}{
		{"success", "", ""},
		{"missing admission webhook", "participant_joined", "join webhook"},
		{"missing departure webhook", "participant_left", "did not clear"},
		{"LiveKit stopped", "", "LiveKit is unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "LiveKit stopped" {
				if err := sfu.Stop(t.Context(), nil); err != nil {
					t.Fatalf("stop smoke LiveKit: %v", err)
				}
			}
			dropped.Store(tc.drop)
			guildCreated.Store(false)
			guildDeleted.Store(false)
			voiceCleaned.Store(false)
			api, _ := newModulesRouter(t, httpx.DeadlineOptions{}, func(cfg *config.Config) {
				cfg.AllowedOrigins = []string{srv.URL}
				cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret = endpoint, key, secret
			})
			active.Store(api)
			var out, errOut bytes.Buffer
			code := runSmoke(t.Context(), srv.URL, srv.URL, smokeOptions{Voice: true}, &out, &errOut)
			wantCode := 1
			if tc.want == "" {
				wantCode = 0
			}
			if code != wantCode || !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("voice smoke = %d, stderr %q, stdout %q, want %d and %q", code, errOut.String(), out.String(), wantCode, tc.want)
			}
			if !guildCreated.Load() || !guildDeleted.Load() || !voiceCleaned.Load() {
				t.Errorf("created/deleted/voice cleanup = %v/%v/%v, want all true", guildCreated.Load(), guildDeleted.Load(), voiceCleaned.Load())
			}
			if strings.Contains(out.String()+errOut.String(), secret) || strings.Contains(out.String()+errOut.String(), "eyJ") {
				t.Error("voice credentials appeared in smoke output")
			}
		})
	}
}
