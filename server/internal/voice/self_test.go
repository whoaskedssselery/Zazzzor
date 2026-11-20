package voice_test

import (
	"net/http"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

const (
	selfPath  = "/api/v1/users/@me/voice-state"
	leavePath = "/api/v1/users/@me/voice"
)

type stateBody struct {
	ChannelID string `json:"channel_id"`
	SelfMute  bool   `json:"self_mute"`
	SelfDeaf  bool   `json:"self_deaf"`
}

func TestUpdateSelfOutsideVoiceIsConflict(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	problemOf(t, alice.Do(http.MethodPatch, selfPath, map[string]any{"self_mute": true}), http.StatusConflict, "not_in_voice")
}

func TestUpdateSelfNeedsAField(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	_, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	problemOf(t, alice.Do(http.MethodPatch, selfPath, map[string]any{}), http.StatusBadRequest, "validation_failed")
	problemOf(t, alice.Do(http.MethodPatch, selfPath, map[string]any{"mute": true}), http.StatusBadRequest, "invalid_body")
}

func TestUpdateSelfAnnouncesOnlyChanges(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	_, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")

	resp := alice.Do(http.MethodPatch, selfPath, map[string]any{"self_mute": true})
	expect(t, resp, http.StatusOK)
	var st stateBody
	resp.JSON(&st)
	if !st.SelfMute || st.SelfDeaf || st.ChannelID != vc.ID {
		t.Fatalf("state = %+v, want muted, not deafened, in %s", st, vc.ID)
	}
	expect(t, alice.Do(http.MethodPatch, selfPath, map[string]any{"self_mute": true}), http.StatusOK)
	expect(t, alice.Do(http.MethodPatch, selfPath, map[string]any{"self_deaf": true}), http.StatusOK)

	ups := e.pub.updates()
	if len(ups) != 3 || !ups[1].SelfMute || ups[1].SelfDeaf || !ups[2].SelfDeaf || !ups[2].SelfMute {
		t.Fatalf("updates = %+v, want join, mute, deafen", ups)
	}
}

func TestReconnectKeepsTheSelfFlags(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	expect(t, alice.Do(http.MethodPatch, selfPath, map[string]any{"self_deaf": true}), http.StatusOK)
	e.connect(alice, a, vc.ID, "RM_1", "PA_2")
	if st, _ := e.stateOf(parseID(t, g.ID), a.id(t)); !st.SelfDeaf {
		t.Fatal("reconnecting to the same channel reset self_deaf")
	}
}

func TestLeaveClearsTheStateAndRemovesTheParticipant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.run()
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")

	expect(t, alice.Do(http.MethodDelete, leavePath, nil), http.StatusNoContent)
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("leaving kept the state")
	}
	ups := e.pub.updates()
	if last := ups[len(ups)-1]; !last.Left || last.ChannelID != vc.id(t) {
		t.Fatalf("last update = %+v, want left %s", last, vc.ID)
	}
	e.rooms.waitRemoved(t, vc.ID+"/"+a.ID)

	// The webhook of the removal and a second leave change nothing.
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_1", "")})
	expect(t, alice.Do(http.MethodDelete, leavePath, nil), http.StatusNoContent)
	if n := len(e.pub.updates()); n != len(ups) {
		t.Fatalf("updates = %d after the leave, want %d", n, len(ups))
	}
}

func TestLeaveRevokesAPendingToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	join := claims(t, joinVoice(t, alice, vc.ID).Token).Attributes["zazzzor.join"]
	expect(t, alice.Do(http.MethodDelete, leavePath, nil), http.StatusNoContent)
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_1", join)})
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("a token issued before leaving still connected")
	}
}

func TestSelfAndLeaveWithoutLiveKitAreUnavailable(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *voice.Options) { o.Rooms, o.APIKey, o.APISecret = nil, "", "" })
	alice, _ := e.user("alice")
	problemOf(t, alice.Do(http.MethodPatch, selfPath, map[string]any{"self_mute": true}),
		http.StatusServiceUnavailable, "voice_unavailable")
	problemOf(t, alice.Do(http.MethodDelete, leavePath, nil), http.StatusServiceUnavailable, "voice_unavailable")
}
