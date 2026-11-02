package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestServeExportsVoiceFreshnessWithoutBlockingReadiness(t *testing.T) {
	for _, healthy := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable_livekit", true: "healthy_livekit"}[healthy], func(t *testing.T) {
			called := make(chan struct{})
			var once sync.Once
			media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				once.Do(func() { close(called) })
				if !healthy {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/protobuf")
				w.WriteHeader(http.StatusOK) // Empty protobuf ListRoomsResponse.
			}))
			t.Cleanup(media.Close)
			cfg := testConfig()
			cfg.DataDir = t.TempDir()
			cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret = media.URL, "key", "0123456789abcdef0123456789abcdef"
			api, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen API: %v", err)
			}
			t.Cleanup(func() { _ = api.Close() })
			metrics, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen metrics: %v", err)
			}
			t.Cleanup(func() { _ = metrics.Close() })
			pool := testkit.Postgres(t)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- serve(ctx, slog.New(slog.DiscardHandler), cfg, pool, api, metrics) }()
			t.Cleanup(func() {
				cancel()
				select {
				case serveErr := <-done:
					if serveErr != nil {
						t.Errorf("serve shutdown: %v", serveErr)
					}
				case <-time.After(10 * time.Second):
					t.Error("serve shutdown timed out")
				}
			})
			select {
			case <-called:
			case <-time.After(5 * time.Second):
				t.Fatal("initial voice reconcile not started")
			}
			client := &http.Client{Timeout: time.Second}
			const name = "zazzzor_voice_reconcile_last_success_timestamp_seconds"
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				resp, requestErr := client.Get("http://" + metrics.Addr().String() + "/metrics")
				if requestErr != nil {
					t.Fatalf("metrics request: %v", requestErr)
				}
				body, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if readErr != nil {
					t.Fatalf("metrics response: %v", readErr)
				}
				value := -1.0
				for _, line := range strings.Split(string(body), "\n") {
					if raw, ok := strings.CutPrefix(line, name+" "); ok {
						value, err = strconv.ParseFloat(raw, 64)
						if err != nil {
							t.Fatalf("parse freshness: %v", err)
						}
					}
				}
				if value < 0 {
					t.Fatal("voice freshness collector missing from private /metrics")
				}
				if !healthy || value > 0 {
					if !healthy && value != 0 {
						t.Fatalf("unavailable LiveKit freshness=%v, want 0", value)
					}
					break
				}
				select {
				case <-ticker.C:
				case <-deadline.C:
					t.Fatal("successful voice reconcile did not update /metrics")
				}
			}
			resp, err := client.Get("http://" + api.Addr().String() + "/api/v1/readyz")
			if err != nil {
				t.Fatalf("readiness request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("readiness=%d with healthy database, want 200", resp.StatusCode)
			}
		})
	}
}
