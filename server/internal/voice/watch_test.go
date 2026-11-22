package voice_test

import (
	"net/http"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func watchPath(channelID string) string { return "/api/v1/channels/" + channelID + "/voice/watching" }

// watches returns the STREAM_WATCH events so far, each with who it went to.
func (p *recorder) watches() ([]voice.WatchPayload, [][]snowflake.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []voice.WatchPayload
	var to [][]snowflake.ID
	for _, e := range p.events {
		if e.Type != events.StreamWatch {
			continue
		}
		out = append(out, e.Payload.(voice.WatchPayload))
		to = append(to, e.Audience.UserIDs)
	}
	return out, to
}

func TestWatchingAStreamTellsOnlyItsSharer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	g, _, vc := newGuild(t, alice, "home")
	e.join(g, b)
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	e.connect(bob, b, vc.ID, "RM_1", "PB_1")
	e.hook(&livekit.WebhookEvent{Event: "track_published", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_1", ""), Track: &livekit.TrackInfo{Sid: "TR_screen", Source: livekit.TrackSource_SCREEN_SHARE}})

	expect(t, bob.Do(http.MethodPut, watchPath(vc.ID), map[string]any{"user_id": a.ID, "watching": true}), http.StatusNoContent)
	expect(t, bob.Do(http.MethodPut, watchPath(vc.ID), map[string]any{"user_id": a.ID, "watching": false}), http.StatusNoContent)

	got, to := e.pub.watches()
	if len(got) != 2 || !got[0].Watching || got[1].Watching {
		t.Fatalf("watch events = %+v, want on then off", got)
	}
	if got[0].UserID != b.id(t) || got[0].ChannelID != vc.id(t) {
		t.Fatalf("watch event = %+v, want bob in %s", got[0], vc.ID)
	}
	for _, ids := range to {
		if len(ids) != 1 || ids[0] != a.id(t) {
			t.Fatalf("watch event went to %v, want only alice", ids)
		}
	}
}

func TestWatchingNeedsAShareInTheSameCall(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	g, _, vc := newGuild(t, alice, "home")
	e.join(g, b)
	body := map[string]any{"user_id": a.ID, "watching": true}

	// Bob is not in voice.
	problemOf(t, bob.Do(http.MethodPut, watchPath(vc.ID), body), http.StatusConflict, "not_in_voice")

	// Both are in the call, but Alice shares nothing.
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	e.connect(bob, b, vc.ID, "RM_1", "PB_1")
	problemOf(t, bob.Do(http.MethodPut, watchPath(vc.ID), body), http.StatusConflict, "not_streaming")

	// Nobody watches themselves.
	problemOf(t, alice.Do(http.MethodPut, watchPath(vc.ID), body), http.StatusBadRequest, "validation_failed")

	if got, _ := e.pub.watches(); len(got) != 0 {
		t.Fatalf("watch events = %+v, want none", got)
	}
}
