package gateway_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
)

// presenceUpdates returns the PRESENCE_UPDATEs among fs, as "user:status".
func presenceUpdates(t *testing.T, fs []frame) []string {
	t.Helper()
	var out []string
	for _, f := range fs {
		if f.T == events.PresenceUpdate {
			var p presenceBody
			decode(t, f.D, &p)
			out = append(out, p.UserID+":"+p.Status)
		}
	}
	slices.Sort(out)
	return out
}

func onlineIn(st guildStateBody) []string {
	var out []string
	for _, p := range st.Presences {
		if p.Status != gateway.StatusOnline {
			continue
		}
		out = append(out, p.UserID)
	}
	slices.Sort(out)
	return out
}

func sorted(ids ...string) []string {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return ids
}

// TestGuildStateCarriesPresences: READY's guild states list the guild's
// online members.
func TestGuildStateCarriesPresences(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	_, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	e.join(g, cu) // offline

	connect(t, bob)
	_, r := connect(t, alice)
	st, ok := r.guild(g.ID)
	if !ok {
		t.Fatal("READY lacks the guild")
	}
	if got, want := onlineIn(st), sorted(au.ID, bu.ID); !slices.Equal(got, want) {
		t.Fatalf("presences = %v, want the online members %v", got, want)
	}
}

// TestJoinSharesPresence: a member who joins learns who is online from
// their GUILD_CREATE, and the members who shared no server with them learn
// that they are online.
func TestJoinSharesPresence(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)
	drain(e, map[*wsClient]userBody{acl: au, bcl: bu, ccl: cu})

	e.join(g, cu)
	var st guildStateBody
	decode(t, ccl.waitFor(events.GuildCreate).D, &st)
	if got, want := onlineIn(st), sorted(au.ID, bu.ID, cu.ID); !slices.Equal(got, want) {
		t.Fatalf("carol's GUILD_CREATE presences = %v, want %v", got, want)
	}
	for name, cl := range map[string]*wsClient{"alice": acl, "bob": bcl} {
		u := map[string]userBody{"alice": au, "bob": bu}[name]
		if got := presenceUpdates(t, e.untilSentinel(cl, u)); !slices.Equal(got, []string{cu.ID + ":online"}) {
			t.Fatalf("%s got presences %v, want carol online", name, got)
		}
	}
}

// TestLeaveEndsSharedPresence: when a member leaves, the members who share
// no other server with them see them offline, and they see those members
// offline; members still sharing a server see nothing.
func TestLeaveEndsSharedPresence(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	h, _ := newGuild(t, bob, "other")
	e.join(g, bu)
	e.join(g, cu)
	e.join(h, cu) // carol still shares h with bob after leaving g
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)
	drain(e, map[*wsClient]userBody{acl: au, bcl: bu, ccl: cu})

	expect(t, carol.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusNoContent)
	acl.waitFor(events.GuildMemberRemove)
	if got := presenceUpdates(t, e.untilSentinel(acl, au)); !slices.Equal(got, []string{cu.ID + ":offline"}) {
		t.Fatalf("alice got presences %v, want carol offline", got)
	}
	if got := presenceUpdates(t, e.untilSentinel(bcl, bu)); len(got) != 0 {
		t.Fatalf("bob, who still shares a server with carol, got presences %v", got)
	}
	if got := presenceUpdates(t, e.untilSentinel(ccl, cu)); !slices.Equal(got, []string{au.ID + ":offline"}) {
		t.Fatalf("carol got presences %v, want alice offline", got)
	}
}

// drain reads what each client got so far (the presences of the
// connections that came up after it).
func drain(e *env, cls map[*wsClient]userBody) {
	e.t.Helper()
	for cl, u := range cls {
		e.untilSentinel(cl, u)
	}
}
