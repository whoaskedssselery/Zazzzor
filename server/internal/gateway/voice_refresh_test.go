package gateway_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestVoiceRefreshPermissionMatrix(t *testing.T) {
	t.Parallel()
	for _, mechanism := range []string{"everyone", "overwrite"} {
		for _, tc := range []struct {
			name string
			lost permissions.Permission
		}{
			{"view", permissions.ViewChannel}, {"connect", permissions.Connect},
			{"speak", permissions.Speak}, {"stream", permissions.Stream},
		} {
			t.Run(mechanism+"/"+tc.name, func(t *testing.T) {
				e, lk := voiceEnv(t)
				owner, _ := e.user("owner")
				member, u := e.user("member")
				g, _ := newGuild(t, owner, "home")
				e.join(g, u)
				ch := voiceChannel(t, owner, g, "voice")
				e.connectVoice(member, u, ch.ID)
				if mechanism == "everyone" {
					expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/roles/"+g.ID,
						map[string]any{"permissions": perm(permissions.DefaultEveryone &^ tc.lost)}), http.StatusOK)
				} else {
					overwrite(t, owner, ch.ID, u.ID, 1, 0, tc.lost)
				}
				if tc.lost == permissions.ViewChannel || tc.lost == permissions.Connect {
					eventually(t, "revoked voice admission", func() bool {
						return !e.inVoice(g, u) && slices.Contains(lk.removals(), ch.ID+"/"+u.ID)
					})
					return
				}
				eventually(t, "revoked voice source", func() bool {
					p := lk.grants(u.ID)
					if p == nil {
						return false
					}
					mic := slices.Contains(p.GetCanPublishSources(), livekit.TrackSource_MICROPHONE)
					stream := slices.Contains(p.GetCanPublishSources(), livekit.TrackSource_SCREEN_SHARE)
					return mic == (tc.lost != permissions.Speak) && stream == (tc.lost != permissions.Stream)
				})
			})
		}
	}
}

func TestVoiceRefreshKeepsHiddenChannelPrivate(t *testing.T) {
	t.Parallel()
	e, lk := voiceEnv(t)
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
	e.connectVoice(member, u, ch.ID)
	overwrite(t, owner, ch.ID, u.ID, 1, permissions.ViewChannel, permissions.Connect)
	eventually(t, "hidden channel removal", func() bool {
		return !e.inVoice(g, u) && slices.Contains(lk.removals(), ch.ID+"/"+u.ID)
	})
	if got := types(e.untilSentinel(observer, other)); slices.Contains(got, events.VoiceStateUpdate) {
		t.Fatalf("hidden channel state reached non-viewer: %v", got)
	}
}

func TestVoiceRefreshFollowsCategorySyncAndMemberRoles(t *testing.T) {
	t.Parallel()
	for _, category := range []bool{true, false} {
		t.Run(map[bool]string{true: "category_sync", false: "member_roles"}[category], func(t *testing.T) {
			e, lk := voiceEnv(t)
			owner, _ := e.user("owner")
			member, u := e.user("member")
			g, _ := newGuild(t, owner, "home")
			e.join(g, u)
			ch := voiceChannel(t, owner, g, "voice")
			if category {
				resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": "locked", "type": 4})
				expect(t, resp, http.StatusCreated)
				var parent channelBody
				resp.JSON(&parent)
				overwrite(t, owner, parent.ID, g.ID, 0, 0, permissions.Connect)
				e.connectVoice(member, u, ch.ID)
				expect(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+ch.ID,
					map[string]any{"parent_id": parent.ID, "sync_permissions": true}), http.StatusOK)
			} else {
				expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/roles/"+g.ID,
					map[string]any{"permissions": perm(permissions.DefaultEveryone &^ permissions.Connect)}), http.StatusOK)
				resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles", map[string]any{"name": "voice", "permissions": perm(permissions.Connect)})
				expect(t, resp, http.StatusCreated)
				var role struct {
					ID string `json:"id"`
				}
				resp.JSON(&role)
				path := "/api/v1/guilds/" + g.ID + "/members/" + u.ID
				expect(t, owner.Do(http.MethodPatch, path, map[string]any{"roles": []string{role.ID}}), http.StatusOK)
				e.connectVoice(member, u, ch.ID)
				expect(t, owner.Do(http.MethodPatch, path, map[string]any{"roles": []string{}}), http.StatusOK)
			}
			eventually(t, "permission sync removal", func() bool {
				return !e.inVoice(g, u) && slices.Contains(lk.removals(), ch.ID+"/"+u.ID)
			})
		})
	}
}
