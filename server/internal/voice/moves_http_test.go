package voice_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestModerationHTTPMovesAndDisconnects(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("owner")
	member, u := e.user("member")
	g, _, source := newGuild(t, owner, "home")
	e.join(g, u)
	target := createVoiceChannel(t, owner, g, "target")
	e.connect(member, u, source.ID, "RM_source", "PA_source")
	path := moderationPath(g.ID, u.ID)
	problemOf(t, member.Do(http.MethodPatch, path, map[string]any{"channel_id": target.ID}), http.StatusForbidden, "missing_permission")
	expect(t, owner.Do(http.MethodPatch, path, map[string]any{"channel_id": target.ID}), http.StatusNoContent)
	if st, _ := e.stateOf(parseID(t, g.ID), u.id(t)); st.ChannelID != source.id(t) {
		t.Fatalf("pending move state=%+v, want source", st)
	}
	expect(t, owner.Do(http.MethodPatch, path, []byte(`{"channel_id":null,"mute":true}`)), http.StatusNoContent)
	if _, ok := e.stateOf(parseID(t, g.ID), u.id(t)); ok {
		t.Fatal("null channel_id did not disconnect")
	}
	e.connect(member, u, target.ID, "RM_target", "PA_target")
	if st, _ := e.stateOf(parseID(t, g.ID), u.id(t)); !st.Mute {
		t.Fatal("combined disconnect lost its server mute")
	}
}

func TestModerationHTTPRejectsInvalidMoveDestinations(t *testing.T) {
	e := newEnv(t)
	owner, u := e.user("owner")
	g, _, source := newGuild(t, owner, "home")
	e.connect(owner, u, source.ID, "RM_1", "PA_1")
	for _, body := range []string{`{"channel_id":0}`, `{"channel_id":"0"}`, `{"channel_id":"01"}`, `{"channel_id":"-1"}`, `{"channel_id":true}`, `{"channel_id":{}}`} {
		t.Run(body, func(t *testing.T) {
			expect(t, owner.Do(http.MethodPatch, moderationPath(g.ID, u.ID), []byte(body)), http.StatusBadRequest)
		})
	}
}

func TestModerationHTTPMoveRequiresDestinationConnect(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("owner")
	member, u := e.user("member")
	g, _, source := newGuild(t, owner, "home")
	e.join(g, u)
	target := createVoiceChannel(t, owner, g, "target")
	e.connect(member, u, source.ID, "RM_1", "PA_1")
	overwrite(t, owner, target.ID, u.ID, 1, 0, permissions.Connect)
	problemOf(t, owner.Do(http.MethodPatch, moderationPath(g.ID, u.ID), map[string]any{"channel_id": target.ID}), http.StatusForbidden, "missing_permission")
}
