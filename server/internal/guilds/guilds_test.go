package guilds_test

import (
	"errors"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func TestCreateGuildBootstrapsEveryoneRole(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, u := e.user("alice")
	g := createGuild(t, c, "  Friends  ")
	if g.Name != "Friends" || g.OwnerID != u.ID || g.IconURL != nil {
		t.Fatalf("guild = %+v", g)
	}

	roles := listRoles(t, c, g)
	if len(roles) != 1 {
		t.Fatalf("roles = %+v, want only @everyone", roles)
	}
	ev := roles[0]
	if ev.ID != g.ID || ev.Name != "@everyone" || ev.Position != 0 || ev.Permissions != perm(permissions.DefaultEveryone) {
		t.Fatalf("@everyone = %+v", ev)
	}

	resp := c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/members", nil)
	expect(t, resp, http.StatusOK)
	var ms []memberBody
	resp.JSON(&ms)
	if len(ms) != 1 || ms[0].User.ID != u.ID || len(ms[0].Roles) != 0 || ms[0].Nick != nil {
		t.Fatalf("members = %+v", ms)
	}

	resp = c.Do(http.MethodGet, "/api/v1/users/@me/guilds", nil)
	expect(t, resp, http.StatusOK)
	var mine []guildBody
	resp.JSON(&mine)
	if len(mine) != 1 || mine[0].ID != g.ID {
		t.Fatalf("my guilds = %+v", mine)
	}

	if len(e.hook.calls) != 1 || e.hook.calls[0] != g.id(t) || !e.hook.inTx {
		t.Fatalf("bootstrapper calls = %v, saw guild in tx = %v", e.hook.calls, e.hook.inTx)
	}
	created := e.pub.ofType(events.GuildCreate)
	if len(created) != 1 || created[0].Audience.Kind != events.AudienceUsers ||
		!slices.Equal(created[0].Audience.UserIDs, []snowflake.ID{u.id(t)}) {
		t.Fatalf("GUILD_CREATE = %+v", created)
	}

	// A failing bootstrapper rolls the whole creation back.
	e.hook.fail = true
	expect(t, c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": "Broken"}), http.StatusInternalServerError)
	resp = c.Do(http.MethodGet, "/api/v1/users/@me/guilds", nil)
	resp.JSON(&mine)
	if len(mine) != 1 {
		t.Fatalf("guilds after failed bootstrap = %+v", mine)
	}
}

func TestCreateGuildValidatesName(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, _ := e.user("alice")
	for name, v := range map[string]string{
		"too short":     "a",
		"too long":      strings.Repeat("я", 101),
		"invisible":     string(rune(0x200B)) + string(rune(0x3164)) + "  ",
		"bidi override": "evil" + string(rune(0x202E)) + "gpj",
	} {
		p := problemOf(t, c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": v}), http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "name" {
			t.Fatalf("%s: errors = %+v", name, p.Errors)
		}
	}
	createGuild(t, c, strings.Repeat("я", 100))
}

func TestGuildLimitPerUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, u := e.user("alice")
	for i := range guilds.MaxGuildsPerUser - 1 {
		createGuild(t, c, "guild "+string(rune('a'+i%26)))
	}
	// Joining counts too: the hundredth guild comes from someone else.
	other, _ := e.user("bob")
	g := createGuild(t, other, "bob's")
	e.join(g.id(t), u.id(t))

	problemOf(t, c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": "one too many"}),
		http.StatusBadRequest, "too_many_guilds")
	g2 := createGuild(t, other, "bob's second")
	if err := e.svc.Join(t.Context(), g2.id(t), u.id(t)); !errors.Is(err, guilds.ErrTooManyGuilds) {
		t.Fatalf("join past the limit: %v", err)
	}

	// At the cap, joining a guild one is already in is still a no-op, not
	// an error (an invite used twice).
	if err := e.svc.Join(t.Context(), g.id(t), u.id(t)); err != nil {
		t.Fatalf("re-join at the limit: %v", err)
	}
	var added bool
	err := db.InTx(t.Context(), e.pool, func(tx pgx.Tx) error {
		var addErr error
		added, addErr = e.svc.AddMember(t.Context(), tx, g.id(t), u.id(t))
		return addErr
	})
	if err != nil || added {
		t.Fatalf("AddMember of a member at the limit = %v, %v; want false, nil", added, err)
	}
}

func TestGuildCreationRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *guilds.Options) { o.CreateLimiter = nil })
	c, _ := e.user("alice")
	for range 5 {
		createGuild(t, c, "guild")
	}
	resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": "sixth"})
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
}

func TestWritesRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *guilds.Options) { o.WriteLimiter = ratelimit.New(rate.Every(time.Hour), 2, 2*time.Hour) })
	c, _ := e.user("alice")
	g := createGuild(t, c, "guild")
	path := "/api/v1/guilds/" + g.ID
	expect(t, c.Do(http.MethodPatch, path, map[string]string{"name": "one"}), http.StatusOK)
	createRole(t, c, g, "two", 0)
	problemOf(t, c.Do(http.MethodPatch, path, map[string]string{"name": "three"}), http.StatusTooManyRequests, "rate_limited")
	// Reads are not limited.
	expect(t, c.Do(http.MethodGet, path, nil), http.StatusOK)
}

func TestOnlyMembersSeeGuild(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	stranger, _ := e.user("mallory")
	g := createGuild(t, owner, "secret")
	base := "/api/v1/guilds/" + g.ID
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, base},
		{http.MethodGet, base + "/members"},
		{http.MethodGet, base + "/roles"},
		{http.MethodDelete, base},
		{http.MethodDelete, "/api/v1/users/@me/guilds/" + g.ID},
	} {
		problemOf(t, stranger.Do(tc.method, tc.path, nil), http.StatusNotFound, "not_found")
	}
	problemOf(t, stranger.Do(http.MethodPatch, base, map[string]string{"name": "mine"}), http.StatusNotFound, "not_found")
	problemOf(t, stranger.Do(http.MethodPost, base+"/roles", map[string]string{"name": "x"}), http.StatusNotFound, "not_found")
	// Unknown and malformed IDs look the same.
	problemOf(t, owner.Do(http.MethodGet, "/api/v1/guilds/123", nil), http.StatusNotFound, "not_found")
	problemOf(t, owner.Do(http.MethodGet, "/api/v1/guilds/0123", nil), http.StatusNotFound, "not_found")

	resp := stranger.Do(http.MethodGet, "/api/v1/users/@me/guilds", nil)
	expect(t, resp, http.StatusOK)
	if strings.TrimSpace(string(resp.Body)) != "[]" {
		t.Fatalf("stranger's guilds = %s", resp.Body)
	}
}

func TestUpdateGuildRequiresManageGuild(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	member, mu := e.user("bob")
	g := createGuild(t, owner, "before")
	e.join(g.id(t), mu.id(t))
	path := "/api/v1/guilds/" + g.ID

	problemOf(t, member.Do(http.MethodPatch, path, map[string]string{"name": "hijacked"}), http.StatusForbidden, "missing_permission")
	problemOf(t, member.Do(http.MethodPatch, path, map[string]string{}), http.StatusForbidden, "missing_permission")

	mgr := createRole(t, owner, g, "managers", permissions.ManageGuild)
	expect(t, setRoles(owner, g, mu.ID, mgr.ID), http.StatusOK)
	e.pub.reset()
	resp := member.Do(http.MethodPatch, path, map[string]string{"name": "after"})
	expect(t, resp, http.StatusOK)
	var got guildBody
	resp.JSON(&got)
	if got.Name != "after" {
		t.Fatalf("name = %q", got.Name)
	}
	upd := e.pub.ofType(events.GuildUpdate)
	if len(upd) != 1 || upd[0].Audience.Kind != events.AudienceGuild || upd[0].Audience.GuildID != g.id(t) {
		t.Fatalf("GUILD_UPDATE = %+v", upd)
	}
	problemOf(t, member.Do(http.MethodPatch, path, map[string]string{"name": "x"}), http.StatusBadRequest, "validation_failed")
}

func TestDeleteGuildOnlyOwner(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	admin, au := e.user("bob")
	g := createGuild(t, owner, "doomed")
	e.join(g.id(t), au.id(t))
	adm := createRole(t, owner, g, "admins", permissions.Administrator)
	expect(t, setRoles(owner, g, au.ID, adm.ID), http.StatusOK)
	expect(t, uploadIcon(t, owner, g, pngImage(t, color.RGBA{R: 200, A: 255})), http.StatusOK)

	path := "/api/v1/guilds/" + g.ID
	problemOf(t, admin.Do(http.MethodDelete, path, nil), http.StatusForbidden, "missing_permission")
	e.pub.reset()
	expect(t, owner.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	problemOf(t, owner.Do(http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	problemOf(t, admin.Do(http.MethodGet, path, nil), http.StatusNotFound, "not_found")

	del := e.pub.ofType(events.GuildDelete)
	if len(del) != 1 || del[0].Audience.Kind != events.AudienceUsers {
		t.Fatalf("GUILD_DELETE = %+v", del)
	}
	got := slices.Sorted(slices.Values(del[0].Audience.UserIDs))
	want := slices.Sorted(slices.Values([]snowflake.ID{ou.id(t), au.id(t)}))
	if !slices.Equal(got, want) {
		t.Fatalf("GUILD_DELETE audience = %v, want %v", got, want)
	}
	// The icon files are gone with the guild.
	left, _ := filepath.Glob(filepath.Join(e.dir, "icons", g.ID, "*.png"))
	if len(left) != 0 {
		t.Fatalf("icon files left: %v", left)
	}
}

func TestOwnerCannotLeave(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	member, mu := e.user("bob")
	g := createGuild(t, owner, "home")
	e.join(g.id(t), mu.id(t))

	problemOf(t, owner.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusBadRequest, "owner_cannot_leave")
	e.pub.reset()
	expect(t, member.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusNoContent)
	problemOf(t, member.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusNotFound, "not_found")

	rm := e.pub.ofType(events.GuildMemberRemove)
	if len(rm) != 1 || rm[0].Audience.Kind != events.AudienceGuild || rm[0].Audience.GuildID != g.id(t) {
		t.Fatalf("GUILD_MEMBER_REMOVE = %+v", rm)
	}
	del := e.pub.ofType(events.GuildDelete)
	if len(del) != 1 || !slices.Equal(del[0].Audience.UserIDs, []snowflake.ID{mu.id(t)}) {
		t.Fatalf("GUILD_DELETE = %+v", del)
	}
}

func TestMembersPagination(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	g := createGuild(t, owner, "crowd")
	for _, id := range e.rawUsers(149) {
		e.join(g.id(t), id)
	}
	path := "/api/v1/guilds/" + g.ID + "/members"
	page := func(query string) []memberBody {
		t.Helper()
		resp := owner.Do(http.MethodGet, path+query, nil)
		expect(t, resp, http.StatusOK)
		var ms []memberBody
		resp.JSON(&ms)
		return ms
	}
	first := page("")
	if len(first) != 100 {
		t.Fatalf("first page = %d members, want 100", len(first))
	}
	second := page("?after=" + first[99].User.ID)
	if len(second) != 50 {
		t.Fatalf("second page = %d members, want 50", len(second))
	}
	all := append(first, second...)
	var ids []snowflake.ID
	for _, m := range all {
		ids = append(ids, parseID(t, m.User.ID))
	}
	if !slices.IsSorted(ids) || len(slices.Compact(slices.Clone(ids))) != 150 || !slices.Contains(ids, ou.id(t)) {
		t.Fatal("pages are not 150 distinct ascending members including the owner")
	}
	if got := page("?limit=1000"); len(got) != 150 {
		t.Fatalf("limit=1000 gave %d", len(got))
	}
	if got := page("?limit=7&after=" + first[0].User.ID); len(got) != 7 || got[0].User.ID != first[1].User.ID {
		t.Fatal("limit and after do not combine")
	}
	for _, q := range []string{"?limit=0", "?limit=1001", "?limit=x", "?after=abc"} {
		problemOf(t, owner.Do(http.MethodGet, path+q, nil), http.StatusBadRequest, "validation_failed")
	}
}

func TestCreateRoleCannotGrantMissingPermission(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	mgr, mu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))
	r := createRole(t, owner, g, "managers", permissions.ManageRoles|permissions.KickMembers)
	expect(t, setRoles(owner, g, mu.ID, r.ID), http.StatusOK)

	path := "/api/v1/guilds/" + g.ID + "/roles"
	problemOf(t, mgr.Do(http.MethodPost, path, map[string]string{"name": "gods", "permissions": perm(permissions.Administrator)}),
		http.StatusForbidden, "missing_permission")
	problemOf(t, mgr.Do(http.MethodPost, path, map[string]string{"name": "bans", "permissions": perm(permissions.BanMembers)}),
		http.StatusForbidden, "missing_permission")
	// What the manager holds (including through @everyone) is grantable.
	created := createRole(t, mgr, g, "kickers", permissions.KickMembers|permissions.ViewChannel)
	if created.Position != 1 {
		t.Fatalf("new role position = %d, want 1", created.Position)
	}
	// Unknown bits are refused for everybody.
	problemOf(t, owner.Do(http.MethodPost, path, map[string]string{"name": "x", "permissions": "1099511627776"}),
		http.StatusBadRequest, "validation_failed")
	// Editing: only the changed bits must be held.
	low := createRole(t, owner, g, "low", permissions.BanMembers)
	rpath := path + "/" + low.ID
	expect(t, mgr.Do(http.MethodPatch, rpath, map[string]string{"permissions": perm(permissions.BanMembers | permissions.KickMembers)}), http.StatusOK)
	problemOf(t, mgr.Do(http.MethodPatch, rpath, map[string]string{"permissions": perm(permissions.KickMembers)}),
		http.StatusForbidden, "missing_permission")
	resp := mgr.Do(http.MethodPatch, rpath, map[string]any{"name": "renamed", "color": 0xFF0000})
	expect(t, resp, http.StatusOK)
	var got roleBody
	resp.JSON(&got)
	if got.Name != "renamed" || got.Color != 0xFF0000 || got.Permissions != perm(permissions.BanMembers|permissions.KickMembers) {
		t.Fatalf("patched role = %+v", got)
	}
}

func TestRoleHierarchyEnforced(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	mgr, mu := e.user("bob")
	_, tu := e.user("carol")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))
	e.join(g.id(t), tu.id(t))

	// New roles go right above @everyone: create from the top down.
	top := createRole(t, owner, g, "top", 0)
	mid := createRole(t, owner, g, "mid", permissions.ManageRoles)
	low := createRole(t, owner, g, "low", 0)
	expect(t, setRoles(owner, g, mu.ID, mid.ID), http.StatusOK)
	roles := listRoles(t, owner, g)
	pos := map[string]int{}
	for _, r := range roles {
		pos[r.ID] = r.Position
	}
	if pos[low.ID] != 1 || pos[mid.ID] != 2 || pos[top.ID] != 3 {
		t.Fatalf("positions = %v", pos)
	}

	rolesPath := "/api/v1/guilds/" + g.ID + "/roles/"
	// Own position and above: no edit, no assignment, no deletion.
	for _, id := range []string{mid.ID, top.ID} {
		problemOf(t, mgr.Do(http.MethodPatch, rolesPath+id, map[string]string{"name": "x"}), http.StatusForbidden, "missing_permission")
		problemOf(t, setRoles(mgr, g, tu.ID, id), http.StatusForbidden, "missing_permission")
		problemOf(t, mgr.Do(http.MethodDelete, rolesPath+id, nil), http.StatusForbidden, "missing_permission")
	}
	// Removing a role the manager cannot manage is refused too.
	expect(t, setRoles(owner, g, tu.ID, top.ID), http.StatusOK)
	problemOf(t, setRoles(mgr, g, tu.ID), http.StatusForbidden, "missing_permission")
	// Below: fine, and roles kept unchanged need no rank.
	expect(t, mgr.Do(http.MethodPatch, rolesPath+low.ID, map[string]string{"name": "lower"}), http.StatusOK)
	resp := setRoles(mgr, g, tu.ID, top.ID, low.ID)
	expect(t, resp, http.StatusOK)
	var m memberBody
	resp.JSON(&m)
	if !slices.Equal(slices.Sorted(slices.Values(m.Roles)), slices.Sorted(slices.Values([]string{top.ID, low.ID}))) {
		t.Fatalf("roles = %v", m.Roles)
	}
	// A role of another guild, or @everyone, is not assignable.
	other := createGuild(t, owner, "other")
	foreign := createRole(t, owner, other, "foreign", 0)
	problemOf(t, setRoles(owner, g, tu.ID, foreign.ID), http.StatusBadRequest, "validation_failed")
	problemOf(t, setRoles(owner, g, tu.ID, g.ID), http.StatusBadRequest, "validation_failed")
	problemOf(t, owner.Do(http.MethodPatch, rolesPath+foreign.ID, map[string]string{"name": "x"}), http.StatusNotFound, "not_found")
	// Without MANAGE_ROLES nothing goes.
	expect(t, setRoles(owner, g, mu.ID), http.StatusOK)
	problemOf(t, setRoles(mgr, g, tu.ID, low.ID), http.StatusForbidden, "missing_permission")
	// The owner manages everything, including their own roles.
	expect(t, owner.Do(http.MethodPatch, rolesPath+top.ID, map[string]string{"name": "top!"}), http.StatusOK)
	expect(t, setRoles(owner, g, ou.ID, top.ID, mid.ID), http.StatusOK)
}

func TestReorderRoles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	mgr, mu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))
	c := createRole(t, owner, g, "c", 0)
	m := createRole(t, owner, g, "m", permissions.ManageRoles)
	b := createRole(t, owner, g, "b", 0)
	a := createRole(t, owner, g, "a", 0)
	// Now (top to bottom): c=4, m=3, b=2, a=1.
	expect(t, setRoles(owner, g, mu.ID, m.ID), http.StatusOK)
	path := "/api/v1/guilds/" + g.ID + "/roles"

	order := func(rs []roleBody) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			if r.Position != i {
				t.Fatalf("positions not dense: %+v", rs)
			}
			out[i] = r.Name
		}
		return out
	}

	e.pub.reset()
	resp := mgr.Do(http.MethodPatch, path, []map[string]any{{"id": a.ID, "position": 2}})
	expect(t, resp, http.StatusOK)
	var rs []roleBody
	resp.JSON(&rs)
	if got := order(rs); !slices.Equal(got, []string{"@everyone", "b", "a", "m", "c"}) {
		t.Fatalf("order = %v", got)
	}
	if n := len(e.pub.ofType(events.GuildRoleUpdate)); n != 2 {
		t.Fatalf("GUILD_ROLE_UPDATE count = %d, want 2", n)
	}
	if len(e.pub.ofType(events.GuildSync)) != 1 {
		t.Fatal("no GUILD_SYNC after reorder")
	}

	// Raising a role to or above the manager's own is refused.
	problemOf(t, mgr.Do(http.MethodPatch, path, []map[string]any{{"id": a.ID, "position": 3}}), http.StatusForbidden, "missing_permission")
	problemOf(t, mgr.Do(http.MethodPatch, path, []map[string]any{{"id": c.ID, "position": 1}}), http.StatusForbidden, "missing_permission")
	// @everyone is not movable, positions start at 1, no duplicates.
	problemOf(t, owner.Do(http.MethodPatch, path, []map[string]any{{"id": g.ID, "position": 2}}), http.StatusBadRequest, "validation_failed")
	problemOf(t, owner.Do(http.MethodPatch, path, []map[string]any{{"id": a.ID, "position": 0}}), http.StatusBadRequest, "validation_failed")
	problemOf(t, owner.Do(http.MethodPatch, path, []map[string]any{{"id": a.ID, "position": 1}, {"id": a.ID, "position": 2}}),
		http.StatusBadRequest, "validation_failed")

	// The owner may move anything; a position past the end means last.
	resp = owner.Do(http.MethodPatch, path, []map[string]any{{"id": b.ID, "position": 99}, {"id": c.ID, "position": 1}})
	expect(t, resp, http.StatusOK)
	resp.JSON(&rs)
	if got := order(rs); !slices.Equal(got, []string{"@everyone", "c", "a", "m", "b"}) {
		t.Fatalf("order = %v", got)
	}
	if got := order(listRoles(t, owner, g)); !slices.Equal(got, []string{"@everyone", "c", "a", "m", "b"}) {
		t.Fatalf("stored order = %v", got)
	}
}

func TestConcurrentRoleCreation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "busy")
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := range n {
		wg.Go(func() {
			name := "r" + strconv.Itoa(i)
			resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles", map[string]string{"name": name})
			if resp.Status != http.StatusCreated {
				errs <- string(resp.Body)
			}
		})
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Errorf("concurrent create failed: %s", msg)
	}
	rs := listRoles(t, owner, g)
	if len(rs) != n+1 {
		t.Fatalf("roles = %d, want %d", len(rs), n+1)
	}
	for i, r := range rs {
		if r.Position != i {
			t.Fatalf("positions not dense: %+v", rs)
		}
	}
}

func TestCannotDeleteEveryone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "guild")
	a := createRole(t, owner, g, "a", 0)
	b := createRole(t, owner, g, "b", 0)
	path := "/api/v1/guilds/" + g.ID + "/roles/"
	problemOf(t, owner.Do(http.MethodDelete, path+g.ID, nil), http.StatusBadRequest, "cannot_delete_everyone")
	problemOf(t, owner.Do(http.MethodPatch, path+g.ID, map[string]string{"name": "all"}), http.StatusBadRequest, "validation_failed")
	expect(t, owner.Do(http.MethodPatch, path+g.ID, map[string]string{"permissions": perm(permissions.ViewChannel)}), http.StatusOK)

	expect(t, owner.Do(http.MethodDelete, path+b.ID, nil), http.StatusNoContent)
	_ = a
	rs := listRoles(t, owner, g)
	if len(rs) != 2 || rs[0].ID != g.ID || rs[1].ID != a.ID || rs[1].Position != 1 {
		t.Fatalf("roles after delete = %+v", rs)
	}
	if rs[0].Permissions != perm(permissions.ViewChannel) {
		t.Fatalf("@everyone permissions = %s", rs[0].Permissions)
	}
	problemOf(t, owner.Do(http.MethodDelete, path+b.ID, nil), http.StatusNotFound, "not_found")
}

func TestNicknamePermissions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	mod, mu := e.user("bob")
	plain, pu := e.user("carol")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))
	e.join(g.id(t), pu.id(t))
	path := func(u userBody) string { return "/api/v1/guilds/" + g.ID + "/members/" + u.ID }

	// Own nickname with CHANGE_NICKNAME (in @everyone by default).
	resp := plain.Do(http.MethodPatch, path(pu), map[string]any{"nick": "  Caz  "})
	expect(t, resp, http.StatusOK)
	var m memberBody
	resp.JSON(&m)
	if m.Nick == nil || *m.Nick != "Caz" {
		t.Fatalf("nick = %v", m.Nick)
	}
	expect(t, plain.Do(http.MethodPatch, path(pu), map[string]any{"nick": nil}), http.StatusOK)
	expect(t, plain.Do(http.MethodPatch, path(pu), map[string]any{"nick": ""}), http.StatusOK)
	for _, bad := range []string{strings.Repeat("x", 33), "   ", "a" + string(rune(0x202E)) + "b", string(rune(0x3164))} {
		problemOf(t, plain.Do(http.MethodPatch, path(pu), map[string]any{"nick": bad}), http.StatusBadRequest, "validation_failed")
	}
	// Others' nicknames need MANAGE_NICKNAMES.
	problemOf(t, plain.Do(http.MethodPatch, path(mu), map[string]any{"nick": "x"}), http.StatusForbidden, "missing_permission")

	// Without CHANGE_NICKNAME, not even one's own.
	expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/roles/"+g.ID,
		map[string]string{"permissions": perm(permissions.DefaultEveryone &^ permissions.ChangeNickname)}), http.StatusOK)
	problemOf(t, plain.Do(http.MethodPatch, path(pu), map[string]any{"nick": "me"}), http.StatusForbidden, "missing_permission")

	mods := createRole(t, owner, g, "mods", permissions.ManageNicknames)
	expect(t, setRoles(owner, g, mu.ID, mods.ID), http.StatusOK)
	resp = mod.Do(http.MethodPatch, path(pu), map[string]any{"nick": "renamed"})
	expect(t, resp, http.StatusOK)
	resp.JSON(&m)
	if m.Nick == nil || *m.Nick != "renamed" {
		t.Fatalf("nick = %v", m.Nick)
	}
	// Not the owner, not someone of equal rank.
	problemOf(t, mod.Do(http.MethodPatch, path(ou), map[string]any{"nick": "x"}), http.StatusForbidden, "missing_permission")
	expect(t, setRoles(owner, g, pu.ID, mods.ID), http.StatusOK)
	problemOf(t, mod.Do(http.MethodPatch, path(pu), map[string]any{"nick": "x"}), http.StatusForbidden, "missing_permission")
	// The owner renames anyone; an unknown member is 404.
	expect(t, owner.Do(http.MethodPatch, path(mu), map[string]any{"nick": "Bobby"}), http.StatusOK)
	problemOf(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/123", map[string]any{"nick": "x"}), http.StatusNotFound, "not_found")
}

func TestRoleChangePublishesGuildSync(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	_, mu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))

	ch, cancel := e.bus.Subscribe(64)
	defer cancel()
	waitSync := func(what string) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case ev := <-ch:
				if ev.Type == events.GuildSync {
					if ev.Audience.Kind != events.AudienceGuildSync || ev.Audience.GuildID != g.id(t) {
						t.Fatalf("%s: GUILD_SYNC audience = %+v", what, ev.Audience)
					}
					return
				}
			case <-timeout:
				t.Fatalf("%s: no GUILD_SYNC", what)
			}
		}
	}
	r := createRole(t, owner, g, "r", 0)
	waitSync("create")
	expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/roles/"+r.ID, map[string]string{"permissions": perm(permissions.KickMembers)}), http.StatusOK)
	waitSync("update")
	expect(t, setRoles(owner, g, mu.ID, r.ID), http.StatusOK)
	waitSync("assign")
	expect(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/roles/"+r.ID, nil), http.StatusNoContent)
	waitSync("delete")
	if len(e.pub.ofType(events.GuildMemberUpdate)) != 1 || len(e.pub.ofType(events.GuildRoleDelete)) != 1 {
		t.Fatal("missing GUILD_MEMBER_UPDATE or GUILD_ROLE_DELETE")
	}
}

func TestGuildIcon(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	member, mu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))

	problemOf(t, uploadIcon(t, member, g, pngImage(t, color.Black)), http.StatusForbidden, "missing_permission")
	resp := uploadIcon(t, owner, g, pngImage(t, color.RGBA{G: 200, A: 255}))
	expect(t, resp, http.StatusOK)
	var first guildBody
	resp.JSON(&first)
	if first.IconURL == nil || !strings.HasPrefix(*first.IconURL, "/api/v1/files/icons/"+g.ID+"/") {
		t.Fatalf("icon_url = %v", first.IconURL)
	}
	expect(t, owner.Do(http.MethodGet, *first.IconURL, nil), http.StatusOK)
	if len(e.pub.ofType(events.GuildUpdate)) != 1 {
		t.Fatal("no GUILD_UPDATE for the icon")
	}

	resp = uploadIcon(t, owner, g, pngImage(t, color.RGBA{B: 200, A: 255}))
	expect(t, resp, http.StatusOK)
	var second guildBody
	resp.JSON(&second)
	if *second.IconURL == *first.IconURL {
		t.Fatal("icon did not change")
	}
	left, _ := filepath.Glob(filepath.Join(e.dir, "icons", g.ID, "*.png"))
	if len(left) != 1 {
		t.Fatalf("icon files = %v, want only the current one", left)
	}
	problemOf(t, uploadIcon(t, owner, g, []byte("not an image")), http.StatusBadRequest, "unsupported_image")
	problemOf(t, owner.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/icon", map[string]string{}), http.StatusUnsupportedMediaType, "unsupported_media_type")
	if _, err := os.Stat(filepath.Join(e.dir, "icons", g.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestDirectory(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	_, mu := e.user("bob")
	g := createGuild(t, owner, "guild")
	e.join(g.id(t), mu.id(t))
	r := createRole(t, owner, g, "r", permissions.KickMembers)
	expect(t, setRoles(owner, g, mu.ID, r.ID), http.StatusOK)
	ctx := t.Context()
	gid := g.id(t)

	sub, err := e.svc.Subject(ctx, gid, mu.id(t))
	if err != nil {
		t.Fatal(err)
	}
	if sub.OwnerID != ou.id(t) || sub.Everyone.ID != gid || len(sub.Roles) != 1 ||
		!permissions.Base(sub).Has(permissions.KickMembers|permissions.ViewChannel) {
		t.Fatalf("subject = %+v", sub)
	}
	if _, err = e.svc.Subject(ctx, gid, 42); err != guilds.ErrNotMember { //nolint:errorlint // sentinel returned as is
		t.Fatalf("stranger subject err = %v", err)
	}
	if ok, _ := e.svc.IsMember(ctx, gid, mu.id(t)); !ok {
		t.Fatal("IsMember = false")
	}
	if ids, _ := e.svc.MemberIDs(ctx, gid); len(ids) != 2 {
		t.Fatalf("MemberIDs = %v", ids)
	}
	if ids, _ := e.svc.GuildIDsOf(ctx, mu.id(t)); !slices.Equal(ids, []snowflake.ID{gid}) {
		t.Fatalf("GuildIDsOf = %v", ids)
	}
	snap, err := e.svc.Snapshot(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Guild.ID != gid || len(snap.Roles) != 2 || len(snap.Members) != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if _, err = e.svc.Snapshot(ctx, 42); err != guilds.ErrNotFound { //nolint:errorlint // sentinel returned as is
		t.Fatalf("missing snapshot err = %v", err)
	}
	if err = e.svc.Join(ctx, 42, mu.id(t)); !errors.Is(err, guilds.ErrNotFound) {
		t.Fatalf("join missing guild err = %v", err)
	}
}

func TestAccessForOtherModules(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, ou := e.user("alice")
	_, mu := e.user("bob")
	g := createGuild(t, owner, "guild")
	gid := g.id(t)
	e.join(gid, mu.id(t))
	r := createRole(t, owner, g, "r", permissions.KickMembers)
	expect(t, setRoles(owner, g, mu.ID, r.ID), http.StatusOK)
	ctx := t.Context()

	a, err := e.svc.AccessIn(ctx, e.pool, gid)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.MemberIDs(), []snowflake.ID{ou.id(t), mu.id(t)}) || !a.IsMember(mu.id(t)) || a.IsMember(42) {
		t.Fatalf("members = %v", a.MemberIDs())
	}
	if role, ok := a.Role(gid); !ok || role.Permissions != permissions.DefaultEveryone {
		t.Fatalf("@everyone = %+v, %v", role, ok)
	}
	if _, ok := a.Role(42); ok {
		t.Fatal("unknown role found")
	}
	// Access answers exactly like Subject.
	want, err := e.svc.SubjectIn(ctx, e.pool, gid, mu.id(t))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := a.Subject(mu.id(t))
	if !ok || got.OwnerID != want.OwnerID || got.Everyone != want.Everyone || !slices.Equal(got.Roles, want.Roles) {
		t.Fatalf("Access subject = %+v, Subject = %+v", got, want)
	}
	if _, ok = a.Subject(42); ok {
		t.Fatal("stranger has a subject")
	}
	if _, err = e.svc.AccessIn(ctx, e.pool, 42); !errors.Is(err, guilds.ErrNotFound) {
		t.Fatalf("AccessIn of a missing guild: %v", err)
	}
	if lg, lerr := e.svc.Lookup(ctx, gid); lerr != nil || lg.Name != "guild" {
		t.Fatalf("Lookup = %+v, %v", lg, lerr)
	}
	if _, err = e.svc.Lookup(ctx, 42); !errors.Is(err, guilds.ErrNotFound) {
		t.Fatalf("Lookup of a missing guild: %v", err)
	}
	err = db.InTx(ctx, e.pool, func(tx pgx.Tx) error { return e.svc.LockGuild(ctx, tx, 42) })
	if !errors.Is(err, guilds.ErrNotMember) {
		t.Fatalf("LockGuild of a missing guild: %v", err)
	}
}

func TestPeerIDs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, au := e.user("alice")
	_, bu := e.user("bob")
	carol, cu := e.user("carol")
	_, du := e.user("dave")
	g1 := createGuild(t, alice, "first")
	e.join(g1.id(t), bu.id(t))
	g2 := createGuild(t, carol, "second")
	e.join(g2.id(t), bu.id(t))
	ctx := t.Context()

	// bob shares a guild with everyone but dave; each peer appears once.
	ids, err := e.svc.PeerIDs(ctx, bu.id(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []snowflake.ID{au.id(t), bu.id(t), cu.id(t)}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Fatalf("PeerIDs(bob) = %v, want %v", ids, want)
	}
	if ids, _ = e.svc.PeerIDs(ctx, au.id(t)); !slices.Equal(ids, sorted(au.id(t), bu.id(t))) {
		t.Fatalf("PeerIDs(alice) = %v", ids)
	}
	if ids, _ = e.svc.PeerIDs(ctx, du.id(t)); len(ids) != 0 {
		t.Fatalf("PeerIDs(dave, in no guild) = %v", ids)
	}
}

func sorted(ids ...snowflake.ID) []snowflake.ID {
	slices.Sort(ids)
	return ids
}
