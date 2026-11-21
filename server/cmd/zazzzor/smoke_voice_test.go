package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestSmokeCommandVoiceFlag(t *testing.T) {
	f := &fakeAPI{t: t}
	var voiceRequested atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/guilds/"+fakeGuild+"/channels" {
			voiceRequested.Store(true)
			problem(w, http.StatusServiceUnavailable, "voice_unavailable")
			return
		}
		f.ServeHTTP(w, r)
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	code := smokeCommand([]string{"--voice", srv.URL}, smokeEnv(smokeTestOrigin), &out, &errOut)
	if code != 1 || !voiceRequested.Load() {
		t.Fatalf("voice smoke = %d, requested %v, stderr %q, want 1 and voice request", code, voiceRequested.Load(), errOut.String())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deleted {
		t.Error("smoke guild deleted = false, want true")
	}
}

func TestSmokeSkipsVoiceByDefault(t *testing.T) {
	f := &fakeAPI{t: t}
	var voiceRequested atomic.Bool
	code, _, errOut := runSmokeAgainst(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/voice") || (r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/channels")) {
			voiceRequested.Store(true)
		}
		f.ServeHTTP(w, r)
	}), smokeOptions{})
	if code != 0 || voiceRequested.Load() {
		t.Errorf("default smoke = %d, voice requested %v, stderr %q, want 0 and false", code, voiceRequested.Load(), errOut)
	}
}

func TestSmokeVoiceDisabledCleansUp(t *testing.T) {
	f := &fakeAPI{t: t}
	var left atomic.Bool
	code, _, errOut := runSmokeAgainst(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/guilds/" + fakeGuild + "/channels":
			writeJSON(w, http.StatusCreated, map[string]string{"id": "103"})
		case "POST /api/v1/channels/103/voice":
			problem(w, http.StatusServiceUnavailable, "voice_unavailable")
		case "DELETE /api/v1/users/@me/voice":
			left.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			f.ServeHTTP(w, r)
		}
	}), smokeOptions{Voice: true})
	if code != 1 || !left.Load() || !strings.Contains(errOut, "voice may be off") {
		t.Errorf("disabled smoke = %d, cleanup %v, stderr %q, want 1 and explained cleanup", code, left.Load(), errOut)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deleted {
		t.Error("smoke guild deleted = false, want true")
	}
}

func TestSmokeVoiceStateFiltersEvents(t *testing.T) {
	for _, field := range []string{"op", "t", "guild_id", "channel_id", "user_id", "left", "missing left"} {
		t.Run(field, func(t *testing.T) {
			valid := map[string]any{"op": 0, "t": "VOICE_STATE_UPDATE", "d": map[string]any{
				"guild_id": "100", "channel_id": "103", "user_id": "1", "left": false,
			}}
			switch field {
			case "op":
				valid[field] = 11
			case "t":
				valid[field] = "OTHER"
			default:
				d := valid["d"].(map[string]any)
				switch field {
				case "missing left":
					delete(d, "left")
				case "left":
					d[field] = true
				default:
					d[field] = "other"
				}
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = c.CloseNow() }()
				data, err := json.Marshal(valid)
				if err != nil {
					t.Errorf("encode voice event: %v", err)
					return
				}
				_ = c.Write(r.Context(), websocket.MessageText, data)
				_, _, _ = c.Read(r.Context())
			}))
			defer srv.Close()
			c, response, err := websocket.Dial(t.Context(), srv.URL, nil)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if err != nil {
				t.Fatalf("dial voice gateway: %v", err)
			}
			defer func() { _ = c.CloseNow() }()
			err = smokeVoiceState(context.Background(), c, "100", "103", "1", false, 50*time.Millisecond)
			if err == nil || !strings.Contains(err.Error(), "join webhook") {
				t.Errorf("unrelated event error = %v, want admission timeout", err)
			}
		})
	}
}

func TestSmokeVoiceStateSkipsFramesOfOtherShapes(t *testing.T) {
	frames := []string{
		`{"op":0,"t":"SOME_LIST","d":[1,2,3]}`,
		`{"op":0,"t":"VOICE_STATE_UPDATE","d":{"guild_id":"100","channel_id":"103","user_id":"1","left":"no"}}`,
		`{"op":11,"d":null}`,
		`{"op":0,"t":"VOICE_STATE_UPDATE","d":{"guild_id":"100","channel_id":"103","user_id":"1","left":false}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		for _, f := range frames {
			if err := c.Write(r.Context(), websocket.MessageText, []byte(f)); err != nil {
				return
			}
		}
		_, _, _ = c.Read(r.Context())
	}))
	defer srv.Close()
	c, response, err := websocket.Dial(t.Context(), srv.URL, nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial voice gateway: %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	if err := smokeVoiceState(context.Background(), c, "100", "103", "1", false, time.Second); err != nil {
		t.Errorf("voice state after frames of other shapes = %v, want nil", err)
	}
}

func TestSmokeVoiceStepFollowsWebSocketInOutput(t *testing.T) {
	f := &fakeAPI{t: t}
	code, out, errOut := runSmokeAgainst(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/guilds/" + fakeGuild + "/channels":
			writeJSON(w, http.StatusCreated, map[string]string{"id": "103"})
		case "POST /api/v1/channels/103/voice":
			problem(w, http.StatusServiceUnavailable, "voice_unavailable")
		case "DELETE /api/v1/users/@me/voice":
			w.WriteHeader(http.StatusNoContent)
		default:
			f.ServeHTTP(w, r)
		}
	}), smokeOptions{Voice: true})
	if code != 1 || !strings.Contains(out, "WebSocket: ok") {
		t.Fatalf("smoke = %d, stdout %q, want 1 with the WebSocket step passed", code, out)
	}
	if !strings.Contains(errOut, `step "voice"`) || strings.Contains(errOut, `step "WebSocket"`) {
		t.Errorf("stderr %q, want the failure named by the voice step alone", errOut)
	}
}

func TestSmokeUsageExplainsVoiceLimits(t *testing.T) {
	for _, want := range []string{"--voice", "signalling", "webhook", "NAT", "sound", "screen"} {
		if !strings.Contains(smokeUsage, want) {
			t.Errorf("smokeUsage lacks %q", want)
		}
	}
}

func TestSmokeDisconnectStopsWaitingForBlockedSDK(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	started := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := smokeDisconnect(ctx, func() {
		close(started)
		<-blocked
	})
	if err == nil || !strings.Contains(err.Error(), "leaving LiveKit") {
		t.Errorf("blocked disconnect = %v, want bounded disconnect error", err)
	}
	select {
	case <-started:
	default:
		t.Error("SDK disconnect was not attempted")
	}
}

func TestSmokeCancellationStillDeletesGuildAndVoice(t *testing.T) {
	f := &fakeAPI{t: t}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var left atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/guilds/" + fakeGuild + "/channels":
			writeJSON(w, http.StatusCreated, map[string]string{"id": "103"})
		case "POST /api/v1/channels/103/voice":
			cancel()
			writeJSON(w, http.StatusOK, map[string]string{"url": "ws://127.0.0.1:1", "token": fakeCookie})
		case "DELETE /api/v1/users/@me/voice":
			left.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			f.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	code := runSmoke(ctx, srv.URL, smokeTestOrigin, smokeOptions{Voice: true}, &out, &errOut)
	if code != 1 || !left.Load() {
		t.Errorf("canceled smoke = %d, voice cleanup %v, stderr %q, want 1 and cleanup", code, left.Load(), errOut.String())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deleted {
		t.Error("canceled smoke guild deleted = false, want true")
	}
}
