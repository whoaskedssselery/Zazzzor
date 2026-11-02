package voice_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// hierarchyMembers supplies explicit member ranks without a database.
type hierarchyMembers map[snowflake.ID]permissions.Subject

func (m hierarchyMembers) Subject(_ context.Context, guildID, userID snowflake.ID) (permissions.Subject, error) {
	sub, ok := m[userID]
	if !ok || sub.GuildID != guildID {
		return permissions.Subject{}, guilds.ErrNotMember
	}
	return sub, nil
}

// hierarchySubject builds a valid member at the requested rank.
func hierarchySubject(userID, ownerID snowflake.ID, rank int) permissions.Subject {
	return permissions.Subject{UserID: userID, OwnerID: ownerID, GuildID: 7, Everyone: permissions.Role{ID: 7}, Roles: []permissions.Role{{ID: 100 + userID, Position: rank}}}
}

func TestHierarchyForEveryModerationAction(t *testing.T) {
	on := true
	for _, action := range []struct {
		name   string
		fields voice.ModerationFields
	}{
		{"mute", voice.ModerationFields{Mute: &on}}, {"deaf", voice.ModerationFields{Deaf: &on}}, {"move", voice.ModerationFields{Move: destination(44)}}, {"disconnect", voice.ModerationFields{Move: disconnect()}},
	} {
		for _, tc := range []struct {
			name                  string
			actorID, ownerID      snowflake.ID
			actorRank, targetRank int
			allowed               bool
		}{
			{"lower", 2, 9, 1, 2, false}, {"equal", 2, 9, 2, 2, false}, {"higher", 2, 9, 3, 2, true}, {"owner", 2, 2, 0, 99, true}, {"self", 1, 9, 0, 0, true}, {"administrator_equal", 2, 9, 2, 2, false}, {"target_owner", 2, 1, 99, 0, false},
		} {
			t.Run(action.name+"/"+tc.name, func(t *testing.T) {
				actor := hierarchySubject(tc.actorID, tc.ownerID, tc.actorRank)
				actor.Roles[0].Permissions = permissions.Administrator
				members := hierarchyMembers{1: hierarchySubject(1, tc.ownerID, tc.targetRank)}
				members[tc.actorID] = actor
				e, c, r := moderationEnv(t, permissions.All, func(o *voice.Options) { o.Members = members })
				if tc.actorID == 1 {
					c.subjects = map[snowflake.ID]permissions.Permission{1: permissions.All}
				}
				before := len(r.updates)
				err := e.voice.Moderate(t.Context(), 7, tc.actorID, 1, action.fields)
				if tc.allowed {
					if err != nil {
						t.Fatalf("Moderate: %v", err)
					}
				} else {
					if !errors.Is(err, guilds.ErrHierarchy) {
						t.Fatalf("Moderate error = %v, want role hierarchy", err)
					}
					if len(r.updates) != before || len(r.removals()) != 0 {
						t.Fatalf("unauthorized effects = %d/%v, want none", len(r.updates)-before, r.removals())
					}
				}
			})
		}
	}
}

// A target who left the guild after the voice-state lookup is 404, not 500.
func TestModerationOfATargetWhoLeftTheGuildIsNotFound(t *testing.T) {
	actor := hierarchySubject(2, 9, 3)
	e, _, r := moderationEnv(t, permissions.All, func(o *voice.Options) { o.Members = hierarchyMembers{2: actor} })
	on := true
	before := len(r.updates)
	err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on})
	if !errors.Is(err, channels.ErrNotFound) || len(r.updates) != before {
		t.Fatalf("error/updates = %v/%d, want not found/0", err, len(r.updates)-before)
	}
}

// countingMembers reports every user as a non-member and counts the reads.
type countingMembers struct{ calls int }

func (m *countingMembers) Subject(context.Context, snowflake.ID, snowflake.ID) (permissions.Subject, error) {
	m.calls++
	return permissions.Subject{}, guilds.ErrNotMember
}

func TestNonMemberCheckedBeforeVoiceLookup(t *testing.T) {
	for _, target := range []snowflake.ID{1, 999} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			members := &countingMembers{}
			e, c, _ := moderationEnv(t, permissions.All, func(o *voice.Options) { o.Members = members })
			reads := 0
			c.afterRead = func(snowflake.ID) { reads++ }
			on := true
			err := e.voice.Moderate(t.Context(), 7, 2, target, voice.ModerationFields{Mute: &on})
			if !errors.Is(err, channels.ErrNotFound) || members.calls != 1 || reads != 0 {
				t.Fatalf("error/member reads/channel reads = %v/%d/%d, want not found/1/0", err, members.calls, reads)
			}
		})
	}
}

func TestModerationErrorsKeepHiddenChannelPrivate(t *testing.T) {
	e := newEnv(t)
	owner, ownerUser := e.user("hierarchyowner")
	moderator, modUser := e.user("hierarchymod")
	outsider, _ := e.user("hierarchyoutsider")
	g, _, vc := newGuild(t, owner, "hierarchyhome")
	e.join(g, modUser)
	e.connect(owner, ownerUser, vc.ID, "RM_hierarchy", "PA_hierarchy")
	grantRole(t, owner, g, modUser, permissions.MuteMembers)
	path := moderationPath(g.ID, ownerUser.ID)
	forbidden := moderator.Do(http.MethodPatch, path, map[string]bool{"mute": true})
	problemOf(t, forbidden, http.StatusForbidden, "role_hierarchy")
	notFound := outsider.Do(http.MethodPatch, path, map[string]bool{"mute": true})
	problemOf(t, notFound, http.StatusNotFound, "not_found")
	overwrite(t, owner, vc.ID, g.ID, 0, 0, permissions.ViewChannel)
	hidden := moderator.Do(http.MethodPatch, path, map[string]bool{"mute": true})
	problemOf(t, hidden, http.StatusNotFound, "not_found")
	for _, body := range [][]byte{forbidden.Body, notFound.Body, hidden.Body} {
		if bytes.Contains(body, []byte(vc.ID)) {
			t.Fatalf("error body = %s, want no hidden channel ID", body)
		}
	}
}
