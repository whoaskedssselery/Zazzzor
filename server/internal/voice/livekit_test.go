package voice_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/proto"

	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

const (
	testKey    = "APItest"
	testSecret = "0123456789abcdef0123456789abcdef"
)

// fakeServer answers the LiveKit room service (Twirp, protobuf) with the
// handler for each method; requests must carry an API token signed with
// testSecret.
func fakeServer(t *testing.T, methods map[string]http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := auth.ParseAPIToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			t.Errorf("request without an API token: %v", err)
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		if _, _, err = tok.Verify(testSecret); err != nil {
			t.Errorf("API token not signed with the secret: %v", err)
		}
		h, ok := methods[strings.TrimPrefix(r.URL.Path, "/twirp/livekit.RoomService/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestLiveKitListsParticipants(t *testing.T) {
	joined := time.Date(2026, 10, 31, 12, 0, 0, 123e6, time.UTC)
	url := fakeServer(t, map[string]http.HandlerFunc{
		"ListParticipants": func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req livekit.ListParticipantsRequest
			if err := proto.Unmarshal(body, &req); err != nil || req.GetRoom() != "42" {
				t.Errorf("ListParticipants request = %s (%v), want room 42", body, err)
			}
			b, _ := proto.Marshal(&livekit.ListParticipantsResponse{Participants: []*livekit.ParticipantInfo{{
				Sid: "PA_1", Identity: "7", JoinedAtMs: joined.UnixMilli(),
				Attributes: map[string]string{"zazzzor.join": "gen"},
				Tracks: []*livekit.TrackInfo{
					{Sid: "TR_mic", Source: livekit.TrackSource_MICROPHONE},
					{Sid: "TR_screen", Source: livekit.TrackSource_SCREEN_SHARE},
					{Sid: "TR_screen_audio", Source: livekit.TrackSource_SCREEN_SHARE_AUDIO},
				},
			}}})
			w.Header().Set("Content-Type", "application/protobuf")
			_, _ = w.Write(b)
		},
	})
	ps, err := voice.NewLiveKit(url, testKey, testSecret).ListParticipants(t.Context(), "42")
	if err != nil {
		t.Fatalf("ListParticipants: %v", err)
	}
	want := voice.Participant{SID: "PA_1", Identity: "7", JoinedAt: joined, Join: "gen", Screens: []string{"TR_screen"}}
	if len(ps) != 1 || ps[0].SID != want.SID || ps[0].Identity != want.Identity || !ps[0].JoinedAt.Equal(want.JoinedAt) ||
		ps[0].Join != want.Join || !slices.Equal(ps[0].Screens, want.Screens) {
		t.Fatalf("participants = %+v, want [%+v]", ps, want)
	}
}

func TestLiveKitRemovingAGoneParticipantSucceeds(t *testing.T) {
	url := fakeServer(t, map[string]http.HandlerFunc{
		"RemoveParticipant": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","msg":"participant not found"}`))
		},
	})
	if err := voice.NewLiveKit(url, testKey, testSecret).RemoveParticipant(t.Context(), "42", "7"); err != nil {
		t.Fatalf("RemoveParticipant of a gone participant: %v, want nil", err)
	}
}

func TestLiveKitReportsRemovalFailures(t *testing.T) {
	url := fakeServer(t, map[string]http.HandlerFunc{
		"RemoveParticipant": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"internal","msg":"boom"}`))
		},
	})
	if err := voice.NewLiveKit(url, testKey, testSecret).RemoveParticipant(t.Context(), "42", "7"); err == nil {
		t.Fatal("RemoveParticipant on a failing server: nil error")
	}
}
