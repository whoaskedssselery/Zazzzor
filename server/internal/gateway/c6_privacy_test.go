package gateway_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"slices"
	"testing"
	"time"

	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestVoiceModerationHiddenChannelPrivacyMatrix(t *testing.T) {
	for _, field := range []string{"mute", "deaf", "disconnect", "stream_revoke"} {
		t.Run(field, func(t *testing.T) {
			e, _ := voiceEnv(t)
			owner, _ := e.user("owner")
			member, u := e.user("member")
			outsider, other := e.user("outsider")
			g, _ := newGuild(t, owner, "home")
			e.join(g, u)
			e.join(g, other)
			ch := voiceChannel(t, owner, g, "private")
			overwrite(t, owner, ch.ID, g.ID, 0, 0, permissions.ViewChannel)
			overwrite(t, owner, ch.ID, u.ID, 1, permissions.ViewChannel, 0)
			observer, _ := connect(t, outsider)
			viewer, _ := connect(t, owner)
			e.connectVoice(member, u, ch.ID)
			viewer.waitFor(events.VoiceStateUpdate)
			if field == "stream_revoke" {
				// A real screen publication sets the public flag before STREAM is revoked.
				body, err := protojson.Marshal(&livekit.WebhookEvent{Id: "stream_" + u.ID, Event: "track_published", Room: &livekit.Room{Name: ch.ID, Sid: "RM_" + ch.ID}, Participant: &livekit.ParticipantInfo{Identity: u.ID, Sid: "PA_" + u.ID}, Track: &livekit.TrackInfo{Sid: "SC_1", Source: livekit.TrackSource_SCREEN_SHARE}})
				if err != nil {
					t.Fatalf("encode screen webhook: %v", err)
				}
				sum := sha256.Sum256(body)
				tok, err := lkauth.NewAccessToken(lkKey, lkSecret).SetValidFor(time.Minute).SetSha256(base64.StdEncoding.EncodeToString(sum[:])).ToJWT()
				if err != nil {
					t.Fatalf("sign screen webhook: %v", err)
				}
				hook := testkit.NewClient(t, e.h)
				hook.Origin = ""
				expect(t, hook.Do(http.MethodPost, "/api/v1"+voice.WebhookPath, body, testkit.WithHeader("Content-Type", "application/webhook+json"), testkit.WithHeader("Authorization", tok)), http.StatusNoContent)
				viewer.waitFor(events.VoiceStateUpdate)
				overwrite(t, owner, ch.ID, u.ID, 1, permissions.ViewChannel, permissions.Stream)
			} else {
				body := map[string]any{field: true}
				if field == "disconnect" {
					body = map[string]any{"channel_id": nil}
				}
				expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+u.ID+"/voice", body), http.StatusNoContent)
			}
			var st voiceStateBody
			decode(t, viewer.waitFor(events.VoiceStateUpdate).D, &st)
			// Verify the action occurred before checking the non-viewer stream.
			states := e.m.Voice.GuildStates(parseID(t, g.ID))
			if field == "disconnect" {
				if e.inVoice(g, u) {
					t.Fatal("disconnected participant present, want absent")
				}
			} else {
				if len(states) != 1 {
					t.Fatalf("states = %v, want participant", states)
				}
				got := states[0]
				if (field == "mute" && !got.Mute) || (field == "deaf" && !got.Deaf) || (field == "stream_revoke" && got.SelfStream) {
					t.Fatalf("state = %+v, want applied %s", got, field)
				}
			}
			if got := types(e.untilSentinel(observer, other)); slices.Contains(got, events.VoiceStateUpdate) {
				t.Fatalf("non-viewer events = %v, want no hidden voice state", got)
			}

		})
	}
}

func TestVoiceModerationErrorsDoNotEchoPresenceIDs(t *testing.T) {
	e, _ := voiceEnv(t)
	owner, _ := e.user("owner")
	member, u := e.user("member")
	outsider, other := e.user("outsider")
	g, _ := newGuild(t, owner, "home")
	e.join(g, u)
	e.join(g, other)
	ch := voiceChannel(t, owner, g, "private")
	overwrite(t, owner, ch.ID, g.ID, 0, 0, permissions.ViewChannel)
	overwrite(t, owner, ch.ID, u.ID, 1, permissions.ViewChannel, 0)
	e.connectVoice(member, u, ch.ID)
	path := "/api/v1/guilds/" + g.ID + "/members/" + u.ID + "/voice"
	for _, tc := range []struct {
		name   string
		client *testkit.Client
		status int
	}{{"non_viewer", outsider, http.StatusNotFound}, {"visible_without_permission", member, http.StatusForbidden}} {
		t.Run(tc.name, func(t *testing.T) {
			response := tc.client.Do(http.MethodPatch, path, map[string]bool{"mute": true})
			expect(t, response, tc.status)
			for _, id := range []string{ch.ID, g.ID, u.ID} {
				if bytes.Contains(response.Body, []byte(id)) {
					t.Fatalf("problem body = %q, want no presence ID %s", response.Body, id)
				}
			}
		})
	}
}
