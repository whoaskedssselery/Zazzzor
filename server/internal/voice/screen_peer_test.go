package voice_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/moby/moby/api/types/container"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/testcontainers/testcontainers-go"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// TestScreenPublisherProcess is invoked only inside the SFU network namespace.
// The pinned CLI cannot publish SCREEN_SHARE or consume an API-issued token.
func TestScreenPublisherProcess(t *testing.T) {
	token := os.Getenv("ZAZZZOR_TEST_SCREEN_TOKEN")
	if token == "" {
		t.Skip("container peer helper")
	}
	left := make(chan struct{})
	closed := sync.OnceFunc(func() { close(left) })
	room, err := lksdk.ConnectToRoomWithToken("http://127.0.0.1:7880", token,
		&lksdk.RoomCallback{OnDisconnected: closed}, lksdk.WithAutoSubscribe(false))
	if err != nil {
		t.Fatalf("connect screen peer: %v", err)
	}
	defer room.Disconnect()
	track, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8})
	if err != nil {
		t.Fatalf("create screen track: %v", err)
	}
	defer func() { _ = track.Close() }()
	if _, err = room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{Name: "screen", Source: livekit.TrackSource_SCREEN_SHARE}); err != nil {
		t.Fatalf("publish screen track: %v", err)
	}
	tick := time.NewTicker(time.Second / 30)
	defer tick.Stop()
	for {
		select {
		case <-left:
			return
		case <-t.Context().Done():
			t.Fatalf("screen peer did not disconnect: %v", t.Context().Err())
		case <-tick.C:
			// A VP8 keyframe header drives the RTP path; this test asserts
			// SFU publication and lifecycle, not a decoder or capture quality.
			if err = track.WriteSample(media.Sample{Data: []byte{0x10, 0, 0, 0x9d, 1, 0x2a, 16, 0, 16, 0}, Duration: time.Second / 30}, nil); err != nil {
				t.Fatalf("write screen sample: %v", err)
			}
		}
	}
}

// startScreenPeer runs this package's SDK peer in the existing disposable SFU.
// Linux reuses the test executable; Windows builds the same helper for Linux.
func startScreenPeer(ctx context.Context, t *testing.T, sfu testcontainers.Container, token string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	if runtime.GOOS != "linux" {
		binary = filepath.Join(t.TempDir(), "voice.test")
		//nolint:gosec // G204: only the output path varies, inside t.TempDir; the command and arguments are fixed.
		build := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, ".")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			t.Fatalf("build Linux screen peer: %v: %s", buildErr, output)
		}
	}
	peer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:  "golang:1.27.4",
			Env:    map[string]string{"ZAZZZOR_TEST_SCREEN_TOKEN": token},
			Files:  []testcontainers.ContainerFile{{HostFilePath: binary, ContainerFilePath: "/screen-peer", FileMode: 0o755}},
			Cmd:    []string{"/screen-peer", "-test.run=^TestScreenPublisherProcess$", "-test.timeout=2m"},
			Labels: map[string]string{testkit.Label: "1"},
			HostConfigModifier: func(h *container.HostConfig) {
				h.NetworkMode = container.NetworkMode("container:" + sfu.GetContainerID())
			},
		}, Started: true,
	})
	testcontainers.CleanupContainer(t, peer)
	if err != nil {
		t.Fatalf("start screen peer: %v", err)
	}
}
