package voice_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestScreenTokenBindsToActiveSession(t *testing.T) {
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
	e := newEnv(t)
	c, u := e.user("alice")
	_, _, vc := newGuild(t, c, "home")
	problemOf(t, c.Do(http.MethodPost, joinPath(vc.ID)+"/screen", nil), http.StatusConflict, "not_in_voice")
	e.connect(c, u, vc.ID, "RM_1", "PA_1")
	problemOf(t, c.Do(http.MethodPost, joinPath("42")+"/screen", nil), http.StatusConflict, "not_in_voice")
}

func TestScreenEndpointWithoutVoice(t *testing.T) {
	e := newEnv(t, func(o *voice.Options) { o.APIKey = "" })
	c, _ := e.user("alice")
	problemOf(t, c.Do(http.MethodPost, joinPath("42")+"/screen", nil), http.StatusServiceUnavailable, "voice_unavailable")
}

// screenParticipant builds the companion identity while reusing primary attributes.
func screenParticipant(u userBody, sid, join string) *livekit.ParticipantInfo {
	p := lkParticipant(u, sid, join)
	p.Identity += ".screen"
	return p
}
