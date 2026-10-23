package invites_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func banUser(c *testkit.Client, g guildBody, userID string) *testkit.Response {
	return c.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/bans/"+userID, map[string]any{})
}

func inviteUses(t *testing.T, e *env, code string) int {
	t.Helper()
	var uses int
	if err := e.pool.QueryRow(t.Context(), "SELECT uses FROM invites WHERE code = $1", code).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	return uses
}

func memberCount(t *testing.T, e *env, g guildBody, userID string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE guild_id = $1 AND user_id = $2",
		g.ID, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBannedCannotJoin: a banned user's join is 403 banned and takes no
// use of the invite, even with uses left.
func TestBannedCannotJoin(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_uses": 5})
	expect(t, join(bobC, inv.Code), http.StatusOK)
	expect(t, banUser(owner, g, bob.ID), http.StatusNoContent)

	e.pub.reset()
	problemOf(t, join(bobC, inv.Code), http.StatusForbidden, "banned")
	if uses := inviteUses(t, e, inv.Code); uses != 1 {
		t.Fatalf("uses = %d, want 1 (the refused join took none)", uses)
	}
	if memberCount(t, e, g, bob.ID) != 0 {
		t.Fatal("bob is a member again")
	}
	if n := len(e.pub.ofType(events.GuildMemberAdd)) + len(e.pub.ofType(events.GuildCreate)); n != 0 {
		t.Fatalf("a refused join published %d events", n)
	}
	// A ban of someone who never was a member holds too.
	carolC, carol := e.user("carol")
	expect(t, banUser(owner, g, carol.ID), http.StatusNoContent)
	problemOf(t, join(carolC, inv.Code), http.StatusForbidden, "banned")
	if uses := inviteUses(t, e, inv.Code); uses != 1 {
		t.Fatalf("uses = %d, want 1", uses)
	}
}

// TestUnban: once unbanned the user joins again through an invite.
func TestUnban(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_age_seconds": 0})
	expect(t, banUser(owner, g, bob.ID), http.StatusNoContent)
	problemOf(t, join(bobC, inv.Code), http.StatusForbidden, "banned")

	expect(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/bans/"+bob.ID, nil), http.StatusNoContent)
	expect(t, join(bobC, inv.Code), http.StatusOK)
	if memberCount(t, e, g, bob.ID) != 1 {
		t.Fatal("bob did not join after the unban")
	}
}

// TestConcurrentBanAndJoin: a ban racing the target's invite accept never
// leaves a banned member, whichever commits first.
func TestConcurrentBanAndJoin(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_age_seconds": 0})
	warmPool(t, e, 4)

	for i := range 8 {
		c, u := e.user("racer" + string(rune('a'+i)))
		var (
			wg          sync.WaitGroup
			start       = make(chan struct{})
			joined, bnd *testkit.Response
		)
		wg.Go(func() { <-start; joined = join(c, inv.Code) })
		wg.Go(func() { <-start; bnd = banUser(owner, g, u.ID) })
		close(start)
		wg.Wait()

		expect(t, bnd, http.StatusNoContent)
		if joined.Status != http.StatusOK {
			problemOf(t, joined, http.StatusForbidden, "banned")
		}
		if n := memberCount(t, e, g, u.ID); n != 0 {
			t.Fatalf("round %d: banned %s is a member (join %d)", i, u.Username, joined.Status)
		}
	}
}
