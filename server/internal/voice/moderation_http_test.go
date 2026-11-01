package voice_test

import (
	"net/http"
	"testing"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func moderationPath(guildID, userID string) string {
	return "/api/v1/guilds/" + guildID + "/members/" + userID + "/voice"
}

func TestModerationHTTPChecksPermissionsAndVisibility(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("owner")
	mod, m := e.user("moderator")
	target, u := e.user("target")
	g, _, vc := newGuild(t, owner, "home")
	e.join(g, m)
	e.join(g, u)
	e.connect(target, u, vc.ID, "RM_1", "PA_target")
	path := moderationPath(g.ID, u.ID)
	anonymous := testkit.NewClient(t, e.h)
	problemOf(t, anonymous.Do(http.MethodPatch, path, map[string]bool{"mute": true}), http.StatusUnauthorized, "unauthenticated")
	problemOf(t, mod.Do(http.MethodPatch, path, map[string]bool{"mute": true}), http.StatusForbidden, "missing_permission")
	grantRole(t, owner, g, m, permissions.MuteMembers)
	expect(t, mod.Do(http.MethodPatch, path, map[string]bool{"mute": true}), http.StatusNoContent)
	problemOf(t, mod.Do(http.MethodPatch, path, map[string]bool{"mute": false, "deaf": true}), http.StatusForbidden, "missing_permission")
	st, _ := e.stateOf(parseID(t, g.ID), u.id(t))
	if !st.Mute || st.Deaf {
		t.Fatalf("state after unauthorized combined patch=%+v, want muted only", st)
	}
	grantRole(t, owner, g, m, permissions.DeafenMembers)
	expect(t, mod.Do(http.MethodPatch, path, map[string]bool{"deaf": true}), http.StatusNoContent)
	problemOf(t, mod.Do(http.MethodPatch, path, map[string]bool{"mute": false}), http.StatusForbidden, "missing_permission")
	other, _, _ := newGuild(t, owner, "other")
	problemOf(t, owner.Do(http.MethodPatch, moderationPath(other.ID, u.ID), map[string]bool{"mute": true}), http.StatusNotFound, "not_found")
	overwrite(t, owner, vc.ID, g.ID, 0, 0, permissions.ViewChannel)
	overwrite(t, owner, vc.ID, u.ID, 1, permissions.ViewChannel, 0)
	problemOf(t, mod.Do(http.MethodPatch, path, map[string]bool{"deaf": false}), http.StatusNotFound, "not_found")
	expect(t, owner.Do(http.MethodPatch, path, map[string]bool{"mute": false, "deaf": false}), http.StatusNoContent)
	last := e.pub.events[len(e.pub.events)-1]
	if last.Type != events.VoiceStateUpdate || last.Audience.Kind != events.AudienceChannel || last.Audience.ChannelID != vc.id(t) {
		t.Fatalf("moderation event audience=%+v, want channel viewers", last)
	}
	if err := e.voice.Leave(t.Context(), u.id(t)); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	problemOf(t, owner.Do(http.MethodPatch, path, map[string]bool{"mute": true}), http.StatusNotFound, "not_found")
}

func TestModerationHTTPRejectsInvalidFields(t *testing.T) {
	e := newEnv(t)
	owner, u := e.user("owner")
	g, _, vc := newGuild(t, owner, "home")
	e.connect(owner, u, vc.ID, "RM_1", "PA_owner")
	for _, body := range []string{`{}`, `{"mute":null}`, `{"mute":null,"deaf":true}`, `{"mute":"true"}`, `{"mute":false,"deaf":null}`, `{"mute":true,"extra":true}`} {
		t.Run(body, func(t *testing.T) {
			expect(t, owner.Do(http.MethodPatch, moderationPath(g.ID, u.ID), []byte(body)), http.StatusBadRequest)
		})
	}
}

func TestModerationHTTPUsesTheSharedVoiceWriteLimit(t *testing.T) {
	e := newEnv(t, func(o *voice.Options) { o.WriteLimiter = ratelimit.New(rate.Limit(0), 1, voice.TokenTTL) })
	owner, u := e.user("owner")
	g, _, vc := newGuild(t, owner, "home")
	e.connect(owner, u, vc.ID, "RM_1", "PA_owner") // joining spends the only write
	problemOf(t, owner.Do(http.MethodPatch, moderationPath(g.ID, u.ID), map[string]bool{"mute": true}), http.StatusTooManyRequests, "rate_limited")
}
