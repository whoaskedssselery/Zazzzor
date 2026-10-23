package gateway_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

type banEventBody struct {
	GuildID string   `json:"guild_id"`
	User    userBody `json:"user"`
}

// framesOf returns the frames of type typ among fs.
func framesOf(fs []frame, typ string) []frame {
	var out []frame
	for _, f := range fs {
		if f.T == typ {
			out = append(out, f)
		}
	}
	return out
}

// TestKickRemovesMemberAndNotifies: the others get GUILD_MEMBER_REMOVE, the
// kicked user GUILD_DELETE exactly once and then nothing more of the guild;
// their channel overwrites are gone.
func TestKickRemovesMemberAndNotifies(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, general := newGuild(t, alice, "guild")
	e.join(g, bu)
	e.join(g, cu)
	overwrite(t, alice, general.ID, bu.ID, 1, permissions.SendMessages, 0)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)
	drain(e, map[*wsClient]userBody{acl: au, bcl: bu, ccl: cu})

	expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/members/"+bu.ID, nil), http.StatusNoContent)
	for name, cl := range map[string]*wsClient{"alice": acl, "carol": ccl} {
		var rm struct {
			GuildID string   `json:"guild_id"`
			User    userBody `json:"user"`
		}
		decode(t, cl.waitFor(events.GuildMemberRemove).D, &rm)
		if rm.GuildID != g.ID || rm.User.ID != bu.ID {
			t.Fatalf("%s: GUILD_MEMBER_REMOVE = %+v", name, rm)
		}
	}
	// The guild's traffic must not reach bob any more.
	post(t, alice, general.ID, "after the kick")
	expect(t, alice.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID, map[string]string{"name": "renamed"}), http.StatusOK)
	createChannel(t, alice, g, "new")
	expect(t, carol.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusNoContent)

	got := e.untilSentinel(bcl, bu)
	deletes := 0
	for _, f := range got {
		switch f.T {
		case events.GuildDelete:
			var d struct{ ID string }
			decode(t, f.D, &d)
			if d.ID != g.ID {
				t.Fatalf("GUILD_DELETE of %s", d.ID)
			}
			deletes++
		case events.PresenceUpdate:
		default:
			t.Fatalf("bob got %s after the kick: %s", f.T, f.D)
		}
	}
	if deletes != 1 {
		t.Fatalf("bob got %d GUILD_DELETE, want exactly 1 (%v)", deletes, types(got))
	}
	// Bob shares nothing with alice or carol any more: both go offline
	// for him, he for them.
	if got, want := presenceUpdates(t, got), sorted(au.ID+":offline", cu.ID+":offline"); !slices.Equal(got, want) {
		t.Fatalf("bob got presences %v, want %v", got, want)
	}
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM overwrites WHERE target_id = $1", bu.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d overwrites of bob survive the kick", n)
	}
}

// TestBanEventsOnlyToModerators: GUILD_BAN_ADD and GUILD_BAN_REMOVE reach
// the members holding BAN_MEMBERS when they are sent, nobody else.
func TestBanEventsOnlyToModerators(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	e.join(g, cu)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)
	drain(e, map[*wsClient]userBody{acl: au, bcl: bu, ccl: cu})

	expect(t, alice.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/bans/"+cu.ID,
		map[string]any{"reason": "spam"}), http.StatusNoContent)
	var add banEventBody
	decode(t, acl.waitFor(events.GuildBanAdd).D, &add)
	if add.GuildID != g.ID || add.User.ID != cu.ID {
		t.Fatalf("GUILD_BAN_ADD = %+v", add)
	}
	bobGot := e.untilSentinel(bcl, bu)
	if len(framesOf(bobGot, events.GuildMemberRemove)) != 1 || len(framesOf(bobGot, events.GuildBanAdd)) != 0 {
		t.Fatalf("bob (no BAN_MEMBERS) got %v", types(bobGot))
	}
	carolGot := e.untilSentinel(ccl, cu)
	if len(framesOf(carolGot, events.GuildDelete)) != 1 || len(framesOf(carolGot, events.GuildBanAdd)) != 0 {
		t.Fatalf("carol (banned) got %v", types(carolGot))
	}

	resp := alice.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles",
		map[string]any{"name": "mods", "permissions": perm(permissions.BanMembers)})
	expect(t, resp, http.StatusCreated)
	var mods struct {
		ID string `json:"id"`
	}
	resp.JSON(&mods)
	expect(t, alice.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+bu.ID,
		map[string]any{"roles": []string{mods.ID}}), http.StatusOK)
	drain(e, map[*wsClient]userBody{acl: au, bcl: bu, ccl: cu})

	expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/bans/"+cu.ID, nil), http.StatusNoContent)
	for name, cl := range map[string]*wsClient{"alice": acl, "bob": bcl} {
		var rm banEventBody
		decode(t, cl.waitFor(events.GuildBanRemove).D, &rm)
		if rm.GuildID != g.ID || rm.User.ID != cu.ID {
			t.Fatalf("%s: GUILD_BAN_REMOVE = %+v", name, rm)
		}
	}
	if got := e.untilSentinel(ccl, cu); len(got) != 0 {
		t.Fatalf("carol got %v after the unban", types(got))
	}
}

// TestGuildDeletePresence: deleting a guild ends the shared presence of
// former members who share no other guild, both ways; those still sharing
// one hear nothing.
func TestGuildDeletePresence(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "doomed")
	h, _ := newGuild(t, bob, "other")
	e.join(g, bu)
	e.join(g, cu)
	e.join(h, cu) // bob and carol still share h
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)
	drain(e, map[*wsClient]userBody{acl: au, bcl: bu, ccl: cu})

	expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID, nil), http.StatusNoContent)
	for _, tc := range []struct {
		name string
		cl   *wsClient
		u    userBody
		want []string
	}{
		{"alice", acl, au, sorted(bu.ID+":offline", cu.ID+":offline")},
		{"bob", bcl, bu, []string{au.ID + ":offline"}},
		{"carol", ccl, cu, []string{au.ID + ":offline"}},
	} {
		got := e.untilSentinel(tc.cl, tc.u)
		if len(framesOf(got, events.GuildDelete)) != 1 {
			t.Fatalf("%s got %v, want one GUILD_DELETE", tc.name, types(got))
		}
		if p := presenceUpdates(t, got); !slices.Equal(p, tc.want) {
			t.Fatalf("%s got presences %v, want %v", tc.name, p, tc.want)
		}
	}
}
