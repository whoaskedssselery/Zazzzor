package voice_test

import (
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/proto"

	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestLiveKitUpdatesExplicitParticipantPermissions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		subscribe bool
		sources   []livekit.TrackSource
	}{
		{"cannot_publish", false, nil},
		{"screen_only", false, []livekit.TrackSource{livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO}},
		{"microphone_and_listen", true, []livekit.TrackSource{livekit.TrackSource_MICROPHONE}},
		{"listen_only", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := fakeServer(t, map[string]http.HandlerFunc{
				"UpdateParticipant": func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					var req livekit.UpdateParticipantRequest
					if err := proto.Unmarshal(body, &req); err != nil {
						t.Errorf("decode update: %v", err)
					}
					p := req.GetPermission()
					if req.GetRoom() != "42" || req.GetIdentity() != "7" || p == nil || p.GetCanSubscribe() != tc.subscribe ||
						p.GetCanPublish() != (len(tc.sources) > 0) || p.GetCanPublishData() || p.GetCanUpdateMetadata() ||
						!slices.Equal(p.GetCanPublishSources(), tc.sources) {
						t.Errorf("UpdateParticipant request = %v, want subscribe=%v sources=%v", &req, tc.subscribe, tc.sources)
					}
					b, _ := proto.Marshal(&livekit.ParticipantInfo{Identity: "7"})
					w.Header().Set("Content-Type", "application/protobuf")
					_, _ = w.Write(b)
				},
			})
			if err := voice.NewLiveKit(url, testKey, testSecret).UpdateParticipant(t.Context(), "42", "7",
				voice.ParticipantPermissions{Subscribe: tc.subscribe, Sources: tc.sources}); err != nil {
				t.Fatalf("UpdateParticipant: %v", err)
			}
		})
	}
}

func TestLiveKitReportsPermissionUpdateFailure(t *testing.T) {
	url := fakeServer(t, map[string]http.HandlerFunc{
		"UpdateParticipant": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"unavailable","msg":"offline"}`))
		},
	})
	if err := voice.NewLiveKit(url, testKey, testSecret).UpdateParticipant(t.Context(), "42", "7", voice.ParticipantPermissions{}); err == nil {
		t.Fatal("UpdateParticipant error = nil, want remote failure")
	}
}
