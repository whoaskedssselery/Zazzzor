package guilds_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// lateSubjects resolves permissions through the guilds service, which is
// built after the audit service that needs it.
type lateSubjects struct{ svc *guilds.Service }

func (l *lateSubjects) Subject(ctx context.Context, guildID, userID snowflake.ID) (permissions.Subject, error) {
	sub, err := l.svc.Subject(ctx, guildID, userID)
	if errors.Is(err, guilds.ErrNotMember) {
		return sub, fmt.Errorf("%w: %w", audit.ErrNotFound, err)
	}
	return sub, err
}

// newAuditEnv is an env whose guilds record into an audit log served at
// /guilds/{guild_id}/audit-logs.
func newAuditEnv(t *testing.T) (*env, *audit.Service) {
	t.Helper()
	subjects := &lateSubjects{}
	var log *audit.Service
	e := newEnv(t, func(o *guilds.Options) {
		log = audit.NewService(audit.Options{Pool: o.Pool, IDs: o.IDs, Subjects: subjects, Users: o.Users})
		o.Audit = log
	})
	subjects.svc = e.svc
	audit.Mount(e.authed, log)
	return e, log
}

type auditChange struct {
	Key string `json:"key"`
	Old any    `json:"old_value"`
	New any    `json:"new_value"`
}

type auditEntry struct {
	ID         string            `json:"id"`
	ActionType int               `json:"action_type"`
	UserID     *string           `json:"user_id"`
	TargetID   *string           `json:"target_id"`
	Changes    []auditChange     `json:"changes"`
	Options    map[string]string `json:"options"`
	Reason     *string           `json:"reason"`
}

type auditPage struct {
	Entries []auditEntry `json:"audit_log_entries"`
	Users   []userBody   `json:"users"`
}

func readAudit(t *testing.T, c *testkit.Client, g guildBody, query string) auditPage {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/audit-logs"+query, nil)
	expect(t, resp, http.StatusOK)
	var p auditPage
	resp.JSON(&p)
	return p
}

func actions(p auditPage) []int {
	out := make([]int, len(p.Entries))
	for i, e := range p.Entries {
		out[i] = e.ActionType
	}
	return out
}

func TestAuditLogRecordsModeration(t *testing.T) {
	e, _ := newAuditEnv(t)
	owner, ou := e.user("owner")
	_, bu := e.user("bob")
	_, cu := e.user("carol")
	g := createGuild(t, owner, "mods")
	e.join(g.id(t), bu.id(t))
	e.join(g.id(t), cu.id(t))

	role := createRole(t, owner, g, "helpers", permissions.KickMembers)
	expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/roles/"+role.ID,
		map[string]any{"name": "moderators"}), http.StatusOK)
	expect(t, setRoles(owner, g, bu.ID, role.ID), http.StatusOK)
	expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+bu.ID,
		map[string]any{"nick": "Бобр"}), http.StatusOK)
	expect(t, kick(owner, g, bu.ID), http.StatusNoContent)
	expect(t, ban(owner, g, cu.ID, map[string]any{"reason": "спам"}), http.StatusNoContent)
	expect(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/bans/"+cu.ID, nil), http.StatusNoContent)

	p := readAudit(t, owner, g, "")
	want := []int{int(audit.MemberBanRemove), int(audit.MemberBanAdd), int(audit.MemberKick), int(audit.MemberUpdate),
		int(audit.MemberRoleUpdate), int(audit.RoleUpdate), int(audit.RoleCreate)}
	if got := actions(p); !slices.Equal(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for _, en := range p.Entries {
		if en.UserID == nil || *en.UserID != ou.ID {
			t.Errorf("action %d by %v, want the owner", en.ActionType, en.UserID)
		}
	}
	if b := p.Entries[1]; b.TargetID == nil || *b.TargetID != cu.ID || b.Reason == nil || *b.Reason != "спам" {
		t.Errorf("ban entry = %+v", b)
	}
	if r := p.Entries[4]; len(r.Changes) != 1 || r.Changes[0].Key != "$add" {
		t.Errorf("role update entry = %+v", r)
	}
	if n := p.Entries[3]; len(n.Changes) != 1 || n.Changes[0].Key != "nick" || n.Changes[0].Old != nil ||
		n.Changes[0].New != "Бобр" {
		t.Errorf("nick entry = %+v", n)
	}
	if u := p.Entries[5]; len(u.Changes) != 1 || u.Changes[0].Old != "helpers" || u.Changes[0].New != "moderators" {
		t.Errorf("role rename entry = %+v", u)
	}
	names := map[string]bool{}
	for _, u := range p.Users {
		names[u.Username] = true
	}
	if !names["owner"] || !names["bob"] || !names["carol"] {
		t.Errorf("users = %+v, want owner, bob and carol", p.Users)
	}

	// Filters and pages.
	if got := actions(readAudit(t, owner, g, "?action_type=22")); !slices.Equal(got, []int{22}) {
		t.Errorf("action filter = %v", got)
	}
	if got := readAudit(t, owner, g, "?user_id="+bu.ID); len(got.Entries) != 0 {
		t.Errorf("bob did nothing, got %v", actions(got))
	}
	first := readAudit(t, owner, g, "?limit=3")
	rest := readAudit(t, owner, g, "?before="+first.Entries[2].ID)
	if got := append(actions(first), actions(rest)...); !slices.Equal(got, want) {
		t.Errorf("paged = %v, want %v", got, want)
	}
	for _, q := range []string{"?action_type=21", "?limit=0", "?limit=101", "?before=x"} {
		problemOf(t, owner.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/audit-logs"+q, nil),
			http.StatusBadRequest, "validation_failed")
	}
}

func TestAuditLogNeedsPermission(t *testing.T) {
	e, log := newAuditEnv(t)
	owner, _ := e.user("owner")
	bob, bu := e.user("bob")
	stranger, _ := e.user("stranger")
	g := createGuild(t, owner, "quiet")
	e.join(g.id(t), bu.id(t))

	path := "/api/v1/guilds/" + g.ID + "/audit-logs"
	problemOf(t, bob.Do(http.MethodGet, path, nil), http.StatusForbidden, "missing_permission")
	problemOf(t, stranger.Do(http.MethodGet, path, nil), http.StatusNotFound, "not_found")

	role := createRole(t, owner, g, "readers", permissions.ViewAuditLog)
	expect(t, setRoles(owner, g, bu.ID, role.ID), http.StatusOK)
	if got := actions(readAudit(t, bob, g, "")); len(got) != 2 {
		t.Errorf("bob reads %v, want the role creation and assignment", got)
	}

	// Old entries are pruned.
	n, err := log.Prune(t.Context(), -time.Minute)
	if err != nil || n < 2 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	if got := readAudit(t, bob, g, ""); len(got.Entries) != 0 {
		t.Errorf("after prune: %v", actions(got))
	}
}
