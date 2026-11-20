package guilds_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

type banBody struct {
	User      *userBody `json:"user"`
	Reason    *string   `json:"reason"`
	CreatedAt string    `json:"created_at"`
}

func kick(c *testkit.Client, g guildBody, userID string) *testkit.Response {
	return c.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/members/"+userID, nil)
}

func ban(c *testkit.Client, g guildBody, userID string, body any) *testkit.Response {
	if body == nil {
		body = map[string]any{}
	}
	return c.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/bans/"+userID, body)
}

func isMember(t *testing.T, e *env, g guildBody, userID snowflake.ID) bool {
	t.Helper()
	ok, err := e.svc.IsMember(t.Context(), g.id(t), userID)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestKickRequiresPermission(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bob, bu := e.user("bob")
	_, cu := e.user("carol")
	stranger, _ := e.user("mallory")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))
	e.join(g.id(t), cu.id(t))

	problemOf(t, kick(bob, g, cu.ID), http.StatusForbidden, "missing_permission")
	problemOf(t, kick(stranger, g, cu.ID), http.StatusNotFound, "not_found")
	if !isMember(t, e, g, cu.id(t)) {
		t.Fatal("carol was kicked without permission")
	}
	// Other bits do not do: not even BAN_MEMBERS.
	other := createRole(t, owner, g, "other", permissions.BanMembers|permissions.ManageRoles)
	expect(t, setRoles(owner, g, bu.ID, other.ID), http.StatusOK)
	problemOf(t, kick(bob, g, cu.ID), http.StatusForbidden, "missing_permission")

	kickers := createRole(t, owner, g, "kickers", permissions.KickMembers)
	expect(t, setRoles(owner, g, bu.ID, kickers.ID), http.StatusOK)
	expect(t, kick(bob, g, cu.ID), http.StatusNoContent)
	if isMember(t, e, g, cu.id(t)) {
		t.Fatal("carol is still a member")
	}
	// No longer a member, unknown or malformed: 404.
	problemOf(t, kick(bob, g, cu.ID), http.StatusNotFound, "not_found")
	problemOf(t, kick(bob, g, "123"), http.StatusNotFound, "not_found")
	problemOf(t, kick(bob, g, "x"), http.StatusNotFound, "not_found")
}

func TestKickHierarchy(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	bob, bu := e.user("bob")
	_, cu := e.user("carol")
	_, du := e.user("dave")
	erin, eu := e.user("erin")
	g := createGuild(t, owner, "guild")
	for _, u := range []userBody{bu, cu, du, eu} {
		e.join(g.id(t), u.id(t))
	}
	// New roles go in at position 1: high 3, adm 2, low 1.
	high := createRole(t, owner, g, "high", permissions.KickMembers|permissions.BanMembers)
	adm := createRole(t, owner, g, "adm", permissions.Administrator)
	low := createRole(t, owner, g, "low", permissions.KickMembers)
	expect(t, setRoles(owner, g, bu.ID, high.ID), http.StatusOK)
	expect(t, setRoles(owner, g, cu.ID, high.ID), http.StatusOK)
	expect(t, setRoles(owner, g, du.ID, low.ID), http.StatusOK)
	expect(t, setRoles(owner, g, eu.ID, adm.ID), http.StatusOK)

	// The owner is untouchable, an equal rank too, for kick and ban alike.
	problemOf(t, kick(bob, g, ou.ID), http.StatusForbidden, "role_hierarchy")
	problemOf(t, ban(bob, g, ou.ID, nil), http.StatusForbidden, "role_hierarchy")
	problemOf(t, kick(bob, g, cu.ID), http.StatusForbidden, "role_hierarchy")
	problemOf(t, ban(bob, g, cu.ID, nil), http.StatusForbidden, "role_hierarchy")
	// ADMINISTRATOR does not lift the hierarchy.
	problemOf(t, kick(erin, g, bu.ID), http.StatusForbidden, "role_hierarchy")
	problemOf(t, ban(erin, g, bu.ID, nil), http.StatusForbidden, "role_hierarchy")
	problemOf(t, kick(erin, g, ou.ID), http.StatusForbidden, "role_hierarchy")
	for _, u := range []userBody{ou, bu, cu} {
		if !isMember(t, e, g, u.id(t)) {
			t.Fatalf("%s was removed", u.Username)
		}
	}
	// A lower rank may be removed: dave (low) by erin (adm), erin by bob.
	expect(t, kick(erin, g, du.ID), http.StatusNoContent)
	expect(t, kick(bob, g, eu.ID), http.StatusNoContent)
	// The owner removes anyone.
	expect(t, kick(owner, g, bu.ID), http.StatusNoContent)
	expect(t, ban(owner, g, cu.ID, nil), http.StatusNoContent)
}

func TestKickSelfRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	bob, bu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))
	mods := createRole(t, owner, g, "mods", permissions.KickMembers|permissions.BanMembers)
	expect(t, setRoles(owner, g, bu.ID, mods.ID), http.StatusOK)

	problemOf(t, kick(bob, g, bu.ID), http.StatusBadRequest, "cannot_target_self")
	problemOf(t, ban(bob, g, bu.ID, nil), http.StatusBadRequest, "cannot_target_self")
	problemOf(t, kick(owner, g, ou.ID), http.StatusBadRequest, "cannot_target_self")
	problemOf(t, ban(owner, g, ou.ID, nil), http.StatusBadRequest, "cannot_target_self")
	if !isMember(t, e, g, bu.id(t)) || !isMember(t, e, g, ou.id(t)) {
		t.Fatal("a self-kick removed someone")
	}
}

func TestKickedUserCanRejoin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bob, bu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))

	e.pub.reset()
	expect(t, kick(owner, g, bu.ID), http.StatusNoContent)
	problemOf(t, bob.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusNotFound, "not_found")
	rm := e.pub.ofType(events.GuildMemberRemove)
	if len(rm) != 1 || rm[0].Audience.Kind != events.AudienceGuild {
		t.Fatalf("GUILD_MEMBER_REMOVE = %+v", rm)
	}
	if p := rm[0].Payload.(guilds.MemberRemovePayload); p.User.ID != bu.id(t) {
		t.Fatalf("GUILD_MEMBER_REMOVE payload = %+v", p)
	}
	del := e.pub.ofType(events.GuildDelete)
	if len(del) != 1 || !slices.Equal(del[0].Audience.UserIDs, []snowflake.ID{bu.id(t)}) {
		t.Fatalf("GUILD_DELETE = %+v", del)
	}
	if n := len(e.pub.ofType(events.GuildBanAdd)); n != 0 {
		t.Fatalf("a kick published %d GUILD_BAN_ADD", n)
	}

	e.join(g.id(t), bu.id(t))
	expect(t, bob.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusOK)
}

func TestBanNonMember(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bob, bu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))
	outsider := e.rawUsers(1)[0]

	// Needs BAN_MEMBERS, even for a non-member.
	problemOf(t, ban(bob, g, outsider.String(), nil), http.StatusForbidden, "missing_permission")
	mods := createRole(t, owner, g, "mods", permissions.BanMembers)
	expect(t, setRoles(owner, g, bu.ID, mods.ID), http.StatusOK)

	// The reason is checked like a channel topic.
	for _, bad := range []any{
		strings.Repeat("x", 513),
		"evil" + string(rune(0x202E)) + "gpj.exe",
		"a" + string(rune(0)) + "b",
		string(rune(0x3164)),
		42,
	} {
		resp := ban(bob, g, outsider.String(), map[string]any{"reason": bad})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("reason %q: status %d %s", bad, resp.Status, resp.Body)
		}
	}
	e.pub.reset()
	reason := "  " + strings.Repeat("я", 512) + "  "
	expect(t, ban(bob, g, outsider.String(), map[string]any{"reason": reason}), http.StatusNoContent)
	resp := owner.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/bans/"+outsider.String(), nil)
	expect(t, resp, http.StatusOK)
	var b banBody
	resp.JSON(&b)
	if b.User == nil || b.User.ID != outsider.String() || b.Reason == nil || *b.Reason != strings.TrimSpace(reason) {
		t.Fatalf("ban = %+v", b)
	}
	if n := len(e.pub.ofType(events.GuildMemberRemove)) + len(e.pub.ofType(events.GuildDelete)); n != 0 {
		t.Fatalf("banning a non-member published %d removal events", n)
	}
	add := e.pub.ofType(events.GuildBanAdd)
	if len(add) != 1 {
		t.Fatalf("GUILD_BAN_ADD = %+v", add)
	}
	a := add[0].Audience
	if a.Kind != events.AudienceGuildPermission || a.GuildID != g.id(t) || a.Permission != permissions.BanMembers {
		t.Fatalf("GUILD_BAN_ADD audience = %+v", a)
	}
	if p := add[0].Payload.(guilds.BanPayload); p.GuildID != g.id(t) || p.User.ID != outsider {
		t.Fatalf("GUILD_BAN_ADD payload = %+v", p)
	}

	// A blank reason is no reason; an account that does not exist is 404.
	other := e.rawUsers(1)[0]
	expect(t, ban(owner, g, other.String(), map[string]any{"reason": " \t "}), http.StatusNoContent)
	resp = owner.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/bans/"+other.String(), nil)
	expect(t, resp, http.StatusOK)
	resp.JSON(&b)
	if b.Reason != nil {
		t.Fatalf("blank reason stored as %q", *b.Reason)
	}
	problemOf(t, ban(owner, g, "123", nil), http.StatusNotFound, "not_found")
	// No body at all is a ban without a reason.
	third := e.rawUsers(1)[0]
	expect(t, owner.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/bans/"+third.String(), nil), http.StatusNoContent)
}

func TestBanMemberRemoves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bob, bu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))

	e.pub.reset()
	expect(t, ban(owner, g, bu.ID, map[string]any{"reason": "spam"}), http.StatusNoContent)
	if isMember(t, e, g, bu.id(t)) {
		t.Fatal("bob is still a member")
	}
	problemOf(t, bob.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusNotFound, "not_found")
	var got []string
	e.pub.mu.Lock()
	for _, ev := range e.pub.events {
		got = append(got, ev.Type)
	}
	e.pub.mu.Unlock()
	if want := []string{events.GuildMemberRemove, events.GuildDelete, events.GuildBanAdd}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if err := e.svc.Join(t.Context(), g.id(t), bu.id(t)); !errors.Is(err, guilds.ErrBanned) {
		t.Fatalf("join of a banned user: %v, want ErrBanned", err)
	}
	if isMember(t, e, g, bu.id(t)) {
		t.Fatal("bob joined while banned")
	}
}

func TestBanIdempotent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	_, bu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))

	e.pub.reset()
	expect(t, ban(owner, g, bu.ID, map[string]any{"reason": "first"}), http.StatusNoContent)
	expect(t, ban(owner, g, bu.ID, map[string]any{"reason": "second"}), http.StatusNoContent)
	expect(t, ban(owner, g, bu.ID, map[string]any{"reason": nil}), http.StatusNoContent)
	resp := owner.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/bans/"+bu.ID, nil)
	expect(t, resp, http.StatusOK)
	var b banBody
	resp.JSON(&b)
	if b.Reason == nil || *b.Reason != "first" {
		t.Fatalf("reason = %v, want the first one kept", b.Reason)
	}
	for typ, want := range map[string]int{events.GuildBanAdd: 1, events.GuildMemberRemove: 1, events.GuildDelete: 1} {
		if n := len(e.pub.ofType(typ)); n != want {
			t.Fatalf("%d %s, want %d", n, typ, want)
		}
	}
}

func TestUnbanRequiresBan(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bob, bu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))
	outsider := e.rawUsers(1)[0]
	path := "/api/v1/guilds/" + g.ID + "/bans/" + outsider.String()

	problemOf(t, owner.Do(http.MethodDelete, path, nil), http.StatusNotFound, "not_found")
	expect(t, ban(owner, g, outsider.String(), nil), http.StatusNoContent)
	problemOf(t, bob.Do(http.MethodDelete, path, nil), http.StatusForbidden, "missing_permission")
	e.pub.reset()
	expect(t, owner.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	rm := e.pub.ofType(events.GuildBanRemove)
	if len(rm) != 1 || rm[0].Audience.Kind != events.AudienceGuildPermission ||
		rm[0].Payload.(guilds.BanPayload).User.ID != outsider {
		t.Fatalf("GUILD_BAN_REMOVE = %+v", rm)
	}
	problemOf(t, owner.Do(http.MethodDelete, path, nil), http.StatusNotFound, "not_found")
	e.join(g.id(t), outsider)
}

func TestBanListAndGet(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		now = now.Add(time.Second)
		return now
	}
	e := newEnv(t, func(o *guilds.Options) { o.Now = clock })
	owner, _ := e.user("alice")
	bob, bu := e.user("bob")
	stranger, _ := e.user("mallory")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), bu.id(t))
	list := "/api/v1/guilds/" + g.ID + "/bans"

	resp := owner.Do(http.MethodGet, list, nil)
	expect(t, resp, http.StatusOK)
	if strings.TrimSpace(string(resp.Body)) != "[]" {
		t.Fatalf("empty list = %s", resp.Body)
	}
	raw := e.rawUsers(3)
	for i, id := range []snowflake.ID{raw[2], raw[0], raw[1]} {
		body := map[string]any{}
		if i == 1 {
			body["reason"] = "flood"
		}
		expect(t, ban(owner, g, id.String(), body), http.StatusNoContent)
	}
	resp = owner.Do(http.MethodGet, list, nil)
	expect(t, resp, http.StatusOK)
	var bans []banBody
	resp.JSON(&bans)
	var ids []string
	for _, b := range bans {
		if b.User == nil {
			t.Fatalf("ban without user: %+v", b)
		}
		ids = append(ids, b.User.ID)
	}
	if want := []string{raw[2].String(), raw[0].String(), raw[1].String()}; !slices.Equal(ids, want) {
		t.Fatalf("bans = %v, want by creation %v", ids, want)
	}
	if bans[1].Reason == nil || *bans[1].Reason != "flood" || bans[0].Reason != nil {
		t.Fatalf("reasons = %v, %v", bans[0].Reason, bans[1].Reason)
	}

	// BAN_MEMBERS for both; outsiders see nothing.
	problemOf(t, bob.Do(http.MethodGet, list, nil), http.StatusForbidden, "missing_permission")
	problemOf(t, bob.Do(http.MethodGet, list+"/"+raw[0].String(), nil), http.StatusForbidden, "missing_permission")
	problemOf(t, stranger.Do(http.MethodGet, list, nil), http.StatusNotFound, "not_found")
	problemOf(t, stranger.Do(http.MethodGet, list+"/"+raw[0].String(), nil), http.StatusNotFound, "not_found")
	// Not banned, unknown, malformed: 404.
	problemOf(t, owner.Do(http.MethodGet, list+"/"+bu.ID, nil), http.StatusNotFound, "not_found")
	problemOf(t, owner.Do(http.MethodGet, list+"/123", nil), http.StatusNotFound, "not_found")
	problemOf(t, owner.Do(http.MethodGet, list+"/x", nil), http.StatusNotFound, "not_found")
	mods := createRole(t, owner, g, "mods", permissions.BanMembers)
	expect(t, setRoles(owner, g, bu.ID, mods.ID), http.StatusOK)
	resp = bob.Do(http.MethodGet, list+"/"+raw[0].String(), nil)
	expect(t, resp, http.StatusOK)
	var one banBody
	resp.JSON(&one)
	if one.User == nil || one.User.ID != raw[0].String() || one.Reason == nil || one.CreatedAt != bans[1].CreatedAt {
		t.Fatalf("ban = %+v", one)
	}
}

func TestKickAndBanRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *guilds.Options) { o.WriteLimiter = ratelimit.New(rate.Every(time.Hour), 2, 2*time.Hour) })
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "guild")
	raw := e.rawUsers(3)
	for _, id := range raw {
		e.join(g.id(t), id)
	}
	expect(t, kick(owner, g, raw[0].String()), http.StatusNoContent)
	expect(t, ban(owner, g, raw[1].String(), nil), http.StatusNoContent)
	problemOf(t, kick(owner, g, raw[2].String()), http.StatusTooManyRequests, "rate_limited")
	problemOf(t, ban(owner, g, raw[2].String(), nil), http.StatusTooManyRequests, "rate_limited")
	problemOf(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/bans/"+raw[1].String(), nil),
		http.StatusTooManyRequests, "rate_limited")
	// Reads are not limited.
	expect(t, owner.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/bans", nil), http.StatusOK)
}
