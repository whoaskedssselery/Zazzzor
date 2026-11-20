package voice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/moby/moby/api/types/container"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// microphoneFixture is ten minutes of valid Opus silence. The CLI paces Ogg
// samples in real time, so a track disappearing during moderation cannot be EOF.
func microphoneFixture(t *testing.T) []byte {
	t.Helper()
	var audio bytes.Buffer
	writer, err := oggwriter.NewWith(&audio, 48000, 1)
	if err != nil {
		t.Fatalf("create microphone fixture: %v", err)
	}
	for i := uint32(0); i < 30000; i++ {
		if err = writer.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: uint16(i), Timestamp: i * 960, SSRC: 1}, Payload: []byte{0xf8, 0xff, 0xfe}}); err != nil {
			t.Fatalf("write microphone fixture: %v", err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close microphone fixture: %v", err)
	}
	return audio.Bytes()
}

// awaitPeerLog waits for the CLI's positive subscription callback evidence.
func awaitPeerLog(ctx context.Context, t *testing.T, peer testcontainers.Container, want string) {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		logs, err := peer.Logs(ctx)
		if err != nil {
			t.Fatalf("read peer logs: %v", err)
		}
		body, err := io.ReadAll(logs)
		_ = logs.Close()
		if err != nil {
			t.Fatalf("read peer output: %v", err)
		}
		if strings.Contains(string(body), want) {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("peer log missing %q: %s", want, body)
		case <-ctx.Done():
			t.Fatalf("wait peer log: %v", ctx.Err())
		}
	}
}

// awaitLiveSubscriptions observes the SFU's active audio subscription gauge. This
// measures server downtrack lifecycle independently of CLI callback metadata.
func awaitLiveSubscriptions(ctx context.Context, t *testing.T, endpoint string, want float64) {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	last := "no metrics"
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
		if err != nil {
			t.Fatalf("build metrics request: %v", err)
		}
		response, err := client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				t.Fatalf("read SFU metrics: %v", readErr)
			}
			for _, line := range strings.Split(string(body), "\n") {
				// The gauge is per track kind; only the microphone series counts.
				if !strings.HasPrefix(line, "livekit_track_subscribed_total{") || !strings.Contains(line, `kind="AUDIO"`) {
					continue
				}
				last = line
				fields := strings.Fields(line)
				if len(fields) != 2 {
					t.Fatalf("subscription metric = %q, want labels and gauge", line)
				}
				count, parseErr := strconv.ParseFloat(fields[1], 64)
				if parseErr != nil {
					t.Fatalf("parse subscription gauge: %v", parseErr)
				}
				if count == want {
					return
				}
			}
		} else {
			last = err.Error()
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("subscription metric = %s, want %v", last, want)
		case <-ctx.Done():
			t.Fatalf("wait subscription metric: %v", ctx.Err())
		}
	}
}

// awaitLiveHook checks state after real callback completion, without sleeping
// or assuming how quickly the SFU delivers its event queue.
func awaitLiveHook(t *testing.T, notices <-chan struct{}, what string, ready func() bool) {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for !ready() {
		select {
		case <-notices:
		case <-timer.C:
			t.Fatalf("timed out waiting for real LiveKit %s", what)
		case <-t.Context().Done():
			t.Fatalf("waiting for real LiveKit %s: %v", what, t.Context().Err())
		}
	}
}

func TestRealLiveKitAdmissionModerationAndRecovery(t *testing.T) {
	t.Parallel()
	pins, err := os.ReadFile("../../../deploy/.env.ci")
	if err != nil {
		t.Fatalf("read LiveKit image pins: %v", err)
	}
	versions := map[string]string{}
	for _, line := range strings.Split(string(pins), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			versions[key] = value
		}
	}
	if versions["LIVEKIT_VERSION"] == "" || versions["LIVEKIT_CLI_VERSION"] == "" {
		t.Fatal("LiveKit server/CLI versions missing from CI pins")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	listenHost, callbackHost := "127.0.0.1", testcontainers.HostInternal
	// In the socket-mounted Linux runner, SFU and test process share Docker's
	// bridge. The pinned host-port forwarder instead dials localhost inside
	// the runner, where the daemon's published SSH port does not exist.
	if _, statErr := os.Stat("/.dockerenv"); statErr == nil {
		hostname, hostErr := os.Hostname()
		if hostErr != nil {
			t.Fatalf("test runner hostname: %v", hostErr)
		}
		addresses, lookupErr := net.DefaultResolver.LookupIP(ctx, "ip4", hostname)
		if lookupErr != nil {
			t.Fatalf("test runner bridge address: %v", lookupErr)
		}
		for _, ip := range addresses {
			if !ip.IsLoopback() {
				listenHost, callbackHost = ip.String(), ip.String()
				break
			}
		}
		if listenHost == "127.0.0.1" {
			t.Fatal("test runner has no non-loopback IPv4 callback address")
		}
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("detect containerized test runner: %v", statErr)
	}
	var active atomic.Value
	var leftAccepted atomic.Bool
	notices := make(chan struct{}, 1)
	callback := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, voice.MaxWebhookBytes+1))
		_ = r.Body.Close()
		if readErr != nil {
			http.Error(w, "read callback", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		response := httptest.NewRecorder()
		if handler := active.Load(); handler != nil {
			handler.(http.Handler).ServeHTTP(response, r)
		} else {
			http.Error(response, "initializing", http.StatusServiceUnavailable)
		}
		for key, values := range response.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
		var event struct{ Event string }
		if json.Unmarshal(body, &event) == nil && event.Event == "participant_left" && response.Code == http.StatusNoContent {
			leftAccepted.Store(true)
		}
		select {
		case notices <- struct{}{}:
		default:
		}
	}))
	_ = callback.Listener.Close()
	callback.Listener, err = net.Listen("tcp", net.JoinHostPort(listenHost, "0"))
	if err != nil {
		t.Fatalf("listen for real LiveKit callbacks: %v", err)
	}
	callback.Start()
	t.Cleanup(callback.Close)
	_, portText, err := net.SplitHostPort(callback.Listener.Addr().String())
	if err != nil {
		t.Fatalf("callback address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("callback port: %v", err)
	}
	hostAccessPorts := []int{port}
	if listenHost != "127.0.0.1" {
		hostAccessPorts = nil
	}
	config := fmt.Sprintf(`port: 7880
prometheus_port: 6789
rtc:
  tcp_port: 7881
  udp_port: 7882
  use_external_ip: false
  node_ip: 127.0.0.1
keys:
  %s: %s
webhook:
  api_key: %s
  urls:
    - http://%s:%d/api/v1/livekit/webhook
`, testKey, testSecret, testKey, callbackHost, port)
	sfu, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:           "livekit/livekit-server:" + versions["LIVEKIT_VERSION"],
			Env:             map[string]string{"LIVEKIT_CONFIG": config},
			ExposedPorts:    []string{"7880/tcp", "6789/tcp"},
			HostAccessPorts: hostAccessPorts,
			Labels:          map[string]string{testkit.Label: "1"},
			WaitingFor:      tcwait.ForHTTP("/").WithPort("7880/tcp"),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, sfu)
	if err != nil {
		t.Fatalf("start real LiveKit: %v", err)
	}
	url, err := sfu.PortEndpoint(ctx, "7880/tcp", "http")
	if err != nil {
		t.Fatalf("real LiveKit endpoint: %v", err)
	}
	metricsURL, err := sfu.PortEndpoint(ctx, "6789/tcp", "http")
	if err != nil {
		t.Fatalf("real LiveKit metrics endpoint: %v", err)
	}
	rooms := voice.NewLiveKit(url, testKey, testSecret)
	var options voice.Options
	e := newEnv(t, func(o *voice.Options) { o.Rooms = rooms; options = *o })
	active.Store(e.h)
	owner, user := e.user("owner")
	guild, _, channel := newGuild(t, owner, "real voice")
	join := claims(t, joinVoice(t, owner, channel.ID).Token).Attributes["zazzzor.join"]

	// The trusted CLI uses fake admin credentials and the API-issued join
	// generation. Its peer connection stays inside this disposable SFU's
	// namespace; this is control/admission coverage, not a browser/NAT test.
	peer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "livekit/livekit-cli:" + versions["LIVEKIT_CLI_VERSION"],
			Env: map[string]string{
				"LIVEKIT_URL": "http://127.0.0.1:7880", "LIVEKIT_API_KEY": testKey, "LIVEKIT_API_SECRET": testSecret,
			},
			Files:  []testcontainers.ContainerFile{{Reader: bytes.NewReader(microphoneFixture(t)), ContainerFilePath: "/microphone.ogg", FileMode: 0o644}},
			Cmd:    []string{"room", "join", "--identity", user.ID, "--attribute", "zazzzor.join=" + join, "--publish", "/microphone.ogg", "--auto-subscribe", channel.ID},
			Labels: map[string]string{testkit.Label: "1"},
			HostConfigModifier: func(h *container.HostConfig) {
				h.NetworkMode = container.NetworkMode("container:" + sfu.GetContainerID())
			},
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, peer)
	if err != nil {
		t.Fatalf("start real LiveKit participant: %v", err)
	}
	awaitLiveHook(t, notices, "admission", func() bool {
		_, ok := e.stateOf(parseID(t, guild.ID), user.id(t))
		return ok
	})
	listed, err := rooms.ListParticipants(ctx, channel.ID)
	if err != nil || len(listed) != 1 || listed[0].Identity != user.ID || listed[0].Join != join {
		t.Fatalf("real participants = %+v (%v), want issued identity/generation", listed, err)
	}
	remote := lksdk.NewRoomServiceClient(url, testKey, testSecret)
	checkGrants := func(subscribe bool, sources ...livekit.TrackSource) {
		t.Helper()
		participant, getErr := remote.GetParticipant(ctx, &livekit.RoomParticipantIdentity{Room: channel.ID, Identity: user.ID})
		if getErr != nil {
			t.Fatalf("get real participant permissions: %v", getErr)
		}
		p := participant.GetPermission()
		if p == nil || p.CanSubscribe != subscribe || p.CanPublish != (len(sources) > 0) ||
			p.CanPublishData || p.CanUpdateMetadata || !slices.Equal(p.CanPublishSources, sources) {
			t.Fatalf("real permissions = %+v, want subscribe=%v sources=%v and no data/metadata", p, subscribe, sources)
		}
	}
	all := []livekit.TrackSource{livekit.TrackSource_MICROPHONE, livekit.TrackSource_SCREEN_SHARE,
		livekit.TrackSource_SCREEN_SHARE_AUDIO, livekit.TrackSource_CAMERA}
	checkGrants(true, all...)
	// Require an actual published MICROPHONE before shrinking its grants.
	awaitLiveHook(t, notices, "published microphone", func() bool {
		p, getErr := remote.GetParticipant(ctx, &livekit.RoomParticipantIdentity{Room: channel.ID, Identity: user.ID})
		return getErr == nil && slices.ContainsFunc(p.GetTracks(), func(track *livekit.TrackInfo) bool { return track.GetSource() == livekit.TrackSource_MICROPHONE })
	})
	// A second real microphone gives the moderated peer an active subscription.
	speakerClient, speaker := e.user("speaker")
	e.join(guild, speaker)
	speakerJoin := claims(t, joinVoice(t, speakerClient, channel.ID).Token).Attributes["zazzzor.join"]
	speakerPeer, speakerErr := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:  "livekit/livekit-cli:" + versions["LIVEKIT_CLI_VERSION"],
			Env:    map[string]string{"LIVEKIT_URL": "http://127.0.0.1:7880", "LIVEKIT_API_KEY": testKey, "LIVEKIT_API_SECRET": testSecret},
			Files:  []testcontainers.ContainerFile{{Reader: bytes.NewReader(microphoneFixture(t)), ContainerFilePath: "/microphone.ogg", FileMode: 0o644}},
			Cmd:    []string{"room", "join", "--identity", speaker.ID, "--attribute", "zazzzor.join=" + speakerJoin, "--publish", "/microphone.ogg", channel.ID},
			Labels: map[string]string{testkit.Label: "1"},
			HostConfigModifier: func(h *container.HostConfig) {
				h.NetworkMode = container.NetworkMode("container:" + sfu.GetContainerID())
			},
		}, Started: true,
	})
	testcontainers.CleanupContainer(t, speakerPeer)
	if speakerErr != nil {
		t.Fatalf("start real speaker: %v", speakerErr)
	}
	awaitPeerLog(ctx, t, peer, "track subscribed")
	awaitLiveSubscriptions(ctx, t, metricsURL, 1)
	path := moderationPath(guild.ID, user.ID)
	expect(t, owner.Do(http.MethodPatch, path, map[string]bool{"mute": true, "deaf": true}), http.StatusNoContent)
	// Server mute and deafen leave the screen share and the camera.
	checkGrants(false, livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO, livekit.TrackSource_CAMERA)
	awaitLiveHook(t, notices, "muted microphone unpublished", func() bool {
		p, getErr := remote.GetParticipant(ctx, &livekit.RoomParticipantIdentity{Room: channel.ID, Identity: user.ID})
		return getErr == nil && !slices.ContainsFunc(p.GetTracks(), func(track *livekit.TrackInfo) bool { return track.GetSource() == livekit.TrackSource_MICROPHONE })
	})
	// Deafen must remove the existing server subscription while both peers live.
	awaitLiveSubscriptions(ctx, t, metricsURL, 0)
	// Ensure the remote source remains published across deafen.
	speakerInfo, speakerErr := remote.GetParticipant(ctx, &livekit.RoomParticipantIdentity{Room: channel.ID, Identity: speaker.ID})
	if speakerErr != nil || !slices.ContainsFunc(speakerInfo.GetTracks(), func(track *livekit.TrackInfo) bool { return track.GetSource() == livekit.TrackSource_MICROPHONE }) {
		t.Fatalf("speaker after deafen = %+v (%v), want active microphone", speakerInfo, speakerErr)
	}
	if _, speakerErr = remote.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: channel.ID, Identity: speaker.ID}); speakerErr != nil {
		t.Fatalf("remove second speaker: %v", speakerErr)
	}
	awaitLiveHook(t, notices, "second speaker departure", func() bool { _, ok := e.stateOf(parseID(t, guild.ID), speaker.id(t)); return !ok })
	expect(t, owner.Do(http.MethodPatch, path, map[string]bool{"mute": false, "deaf": false}), http.StatusNoContent)
	checkGrants(true, all...)

	// The companion uses the actual API token, including its restricted
	// sources and primary generation, rather than trusted CLI credentials.
	var screenJoin joinBody
	screenResponse := owner.Do(http.MethodPost, joinPath(channel.ID)+"/screen", nil)
	expect(t, screenResponse, http.StatusOK)
	screenResponse.JSON(&screenJoin)
	startScreenPeer(ctx, t, sfu, screenJoin.Token)
	awaitLiveHook(t, notices, "companion screen publication", func() bool {
		state, ok := e.stateOf(parseID(t, guild.ID), user.id(t))
		return ok && state.SelfStream
	})
	screenInfo, screenErr := remote.GetParticipant(ctx, &livekit.RoomParticipantIdentity{Room: channel.ID, Identity: user.ID + ".screen"})
	if screenErr != nil || screenInfo.GetPermission().GetCanSubscribe() ||
		!slices.ContainsFunc(screenInfo.GetTracks(), func(track *livekit.TrackInfo) bool { return track.GetSource() == livekit.TrackSource_SCREEN_SHARE }) {
		t.Fatalf("real companion = %+v (%v), want publishing screen without subscription", screenInfo, screenErr)
	}

	// Reconstruct only the voice service over the same DB and active SFU.
	// No synthetic webhook or in-memory participant snapshot is injected.
	recovered := voice.NewService(options)
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) { voice.MountWebhook(r, recovered) })
	active.Store(http.Handler(router))
	if err := recovered.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile real LiveKit after reconstruction: %v", err)
	}
	states := recovered.GuildStates(parseID(t, guild.ID))
	if len(states) != 1 || states[0].UserID != user.id(t) || states[0].ChannelID != channel.id(t) || !states[0].SelfStream {
		t.Fatalf("recovered states = %+v, want active participant", states)
	}
	checkGrants(true, all...)
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); recovered.Run(workerCtx) }()
	t.Cleanup(func() { stop(); <-done })
	if err := recovered.Leave(ctx, user.id(t)); err != nil {
		t.Fatalf("leave real LiveKit: %v", err)
	}
	awaitLiveHook(t, notices, "remote removal", func() bool {
		participants, listErr := rooms.ListParticipants(ctx, channel.ID)
		return listErr == nil && len(participants) == 0 && leftAccepted.Load()
	})
	if states := recovered.GuildStates(parseID(t, guild.ID)); len(states) != 0 {
		t.Fatalf("states after real departure = %+v, want empty", states)
	}
	if _, err := voice.NewLiveKit(url, testKey, "wrong-test-secret").ListRooms(ctx); err == nil {
		t.Fatal("real LiveKit accepted an incorrect SDK secret")
	}
}
