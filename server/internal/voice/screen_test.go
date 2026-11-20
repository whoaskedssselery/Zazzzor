package voice_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestScreenTokenBindsToActiveSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, u := e.user("alice")
	g, _, vc := newGuild(t, c, "home")
	e.connect(c, u, vc.ID, "RM_1", "PA_1")
	before := len(e.pub.updates())
	var j joinBody
	resp := c.Do(http.MethodPost, joinPath(vc.ID)+"/screen", nil)
	expect(t, resp, http.StatusOK)
	resp.JSON(&j)
	gr := claims(t, j.Token)
	if gr.Identity != u.ID+".screen" || gr.Name != "alice" || gr.Video.Room != vc.ID || j.URL != "wss://zazzzor.test" {
		t.Fatalf("screen identity/name/room/url = %q/%q/%q/%q", gr.Identity, gr.Name, gr.Video.Room, j.URL)
	}
	v := gr.Video
	if !v.RoomJoin || !v.GetCanPublish() || v.GetCanSubscribe() || v.GetCanPublishData() || v.GetCanUpdateOwnMetadata() ||
		!slices.Equal(v.CanPublishSources, []string{"screen_share", "screen_share_audio"}) {
		t.Fatalf("screen grants = %+v, want screen-only publisher", v)
	}
	main := joinVoice(t, c, vc.ID)
	// The active session, rather than a newly issued primary intent, binds it.
	if gr.Attributes["zazzzor.join"] == claims(t, main.Token).Attributes["zazzzor.join"] || gr.Attributes["zazzzor.join"] == "" {
		t.Fatal("screen token did not retain active join generation")
	}
	if len(e.pub.updates()) != before || len(e.voice.GuildStates(parseID(t, g.ID))) != 1 {
		t.Fatal("screen token changed public voice state")
	}
}

func TestScreenEndpointRequiresStream(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	c, u := e.user("bob")
	g, _, vc := newGuild(t, owner, "home")
	e.join(g, u)
	e.connect(c, u, vc.ID, "RM_1", "PA_1")
	overwrite(t, owner, vc.ID, g.ID, 0, 0, permissions.Stream)
	problemOf(t, c.Do(http.MethodPost, joinPath(vc.ID)+"/screen", nil), http.StatusForbidden, "missing_permission")
}

func TestScreenEndpointRequiresVoiceSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, u := e.user("alice")
	_, _, vc := newGuild(t, c, "home")
	problemOf(t, c.Do(http.MethodPost, joinPath(vc.ID)+"/screen", nil), http.StatusConflict, "not_in_voice")
	e.connect(c, u, vc.ID, "RM_1", "PA_1")
	problemOf(t, c.Do(http.MethodPost, joinPath("42")+"/screen", nil), http.StatusConflict, "not_in_voice")
}

func TestScreenEndpointWithoutVoice(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *voice.Options) { o.APIKey = "" })
	c, _ := e.user("alice")
	problemOf(t, c.Do(http.MethodPost, joinPath("42")+"/screen", nil), http.StatusServiceUnavailable, "voice_unavailable")
}

func TestScreenTokenInDirectCallDoesNotRingOrCreatePresence(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, u, _, _, dm := dmPair(t, e)
	e.connect(c, u, dm, "RM_dm", "PA_1")
	before := len(e.pub.calls())
	resp := c.Do(http.MethodPost, joinPath(dm)+"/screen", nil)
	expect(t, resp, http.StatusOK)
	var j joinBody
	resp.JSON(&j)
	p := screenParticipant(u, "SC_1", claims(t, j.Token).Attributes["zazzzor.join"])
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(dm, "RM_dm"), Participant: p})
	e.hook(&livekit.WebhookEvent{Event: "track_published", Room: lkRoom(dm, "RM_dm"), Participant: p,
		Track: &livekit.TrackInfo{Sid: "track", Source: livekit.TrackSource_SCREEN_SHARE}})
	_, states := e.voice.PrivateCalls([]snowflake.ID{parseID(t, dm)})
	if len(e.pub.calls()) != before || len(states) != 1 || !states[0].SelfStream || states[0].UserID != u.id(t) {
		t.Fatal("direct call companion rang or created separate presence")
	}
}

// screenParticipant builds the companion identity while reusing primary attributes.
func screenParticipant(u userBody, sid, join string) *livekit.ParticipantInfo {
	p := lkParticipant(u, sid, join)
	p.Identity += ".screen"
	return p
}
