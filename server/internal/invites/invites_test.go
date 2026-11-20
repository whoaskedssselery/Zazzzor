package invites_test

import (
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/invites"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

var codePattern = regexp.MustCompile(`^[0-9A-Za-z]{8}$`)

func TestInviteCodeFormat(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, 2000)
	counts := make(map[rune]int)
	for range 2000 {
		code := invites.NewCode()
		if !codePattern.MatchString(code) || !invites.ValidCode(code) {
			t.Fatalf("code %q", code)
		}
		if seen[code] {
			t.Fatalf("code %q drawn twice", code)
		}
		seen[code] = true
		for _, r := range code {
			counts[r]++
		}
	}
	// 16000 symbols over 62: every symbol shows up.
	if len(counts) != 62 {
		t.Fatalf("only %d distinct symbols", len(counts))
	}
	for _, bad := range []string{"", "abc", "abcdefghi", "abcd-fgh", "abcdéfgh"} {
		if invites.ValidCode(bad) {
			t.Fatalf("ValidCode(%q) = true", bad)
		}
	}

	e := newEnv(t)
	owner, _ := e.user("alice")
	_, ch := newGuild(t, owner, "home")
	if inv := createInvite(t, owner, ch.ID, nil); !codePattern.MatchString(inv.Code) {
		t.Fatalf("created code %q", inv.Code)
	}
}

func TestInviteJoinFlow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, alice := e.user("alice")
	bobC, bob := e.user("bob")
	carolC, _ := e.user("carol")
	daveC, _ := e.user("dave")
	g, ch := newGuild(t, owner, "home")

	before := time.Now()
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_uses": 2})
	if inv.Guild.ID != g.ID || inv.Guild.Name != "home" || inv.Channel.ID != ch.ID || inv.Channel.Name != ch.Name ||
		inv.Inviter.ID != alice.ID || inv.Uses != 0 || inv.MaxUses != 2 || inv.ExpiresAt == nil {
		t.Fatalf("invite = %+v", inv)
	}
	exp, err := time.Parse(time.RFC3339, *inv.ExpiresAt)
	if err != nil || exp.Before(before.Add(7*24*time.Hour-time.Minute)) || exp.After(time.Now().Add(7*24*time.Hour+time.Minute)) {
		t.Fatalf("expires_at = %s (%v)", *inv.ExpiresAt, err)
	}
	forever := createInvite(t, owner, ch.ID, map[string]any{"max_age_seconds": 0})
	if forever.ExpiresAt != nil || forever.MaxUses != 0 {
		t.Fatalf("unlimited invite = %+v", forever)
	}

	// Anyone signed in can preview; without a session nothing.
	resp := bobC.Do(http.MethodGet, "/api/v1/invites/"+inv.Code, nil)
	expect(t, resp, http.StatusOK)
	var preview inviteBody
	resp.JSON(&preview)
	if preview.Code != inv.Code || preview.Guild.ID != g.ID {
		t.Fatalf("preview = %+v", preview)
	}
	anon := testkit.NewClient(t, e.h)
	problemOf(t, anon.Do(http.MethodGet, "/api/v1/invites/"+inv.Code, nil), http.StatusUnauthorized, "unauthenticated")
	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusNotFound, "not_found")

	e.pub.reset()
	resp = join(bobC, inv.Code)
	expect(t, resp, http.StatusOK)
	var joined guildBody
	resp.JSON(&joined)
	if joined.ID != g.ID {
		t.Fatalf("joined %+v", joined)
	}
	expect(t, bobC.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusOK)
	add := e.pub.ofType(events.GuildMemberAdd)
	created := e.pub.ofType(events.GuildCreate)
	if len(add) != 1 || add[0].ExcludeUserID != bob.id(t) || len(created) != 1 ||
		!slices.Equal(created[0].Audience.UserIDs, []snowflake.ID{bob.id(t)}) {
		t.Fatalf("join events: add %+v, create %+v", add, created)
	}

	expect(t, join(carolC, inv.Code), http.StatusOK)
	problemOf(t, join(daveC, inv.Code), http.StatusGone, "invite_exhausted")
	problemOf(t, daveC.Do(http.MethodGet, "/api/v1/invites/"+inv.Code, nil), http.StatusGone, "invite_exhausted")
	for _, got := range listInvites(t, owner, g) {
		if got.Code == inv.Code && got.Uses != 2 {
			t.Fatalf("uses = %d", got.Uses)
		}
	}

	// Unknown and malformed codes look the same.
	problemOf(t, daveC.Do(http.MethodGet, "/api/v1/invites/zzzzzzzz", nil), http.StatusNotFound, "not_found")
	problemOf(t, join(daveC, "nope"), http.StatusNotFound, "not_found")
	problemOf(t, daveC.Do(http.MethodGet, "/api/v1/invites/"+inv.Code+"x", nil), http.StatusNotFound, "not_found")

	// Creating needs CREATE_INVITE in the channel, and membership.
	problemOf(t, daveC.Do(http.MethodPost, "/api/v1/channels/"+ch.ID+"/invites", map[string]any{}), http.StatusNotFound, "not_found")
	expect(t, owner.Do(http.MethodPut, "/api/v1/channels/"+ch.ID+"/permissions/"+g.ID,
		map[string]any{"type": 0, "deny": strconv.FormatUint(uint64(permissions.CreateInvite), 10)}), http.StatusOK)
	problemOf(t, bobC.Do(http.MethodPost, "/api/v1/channels/"+ch.ID+"/invites", map[string]any{}), http.StatusForbidden, "missing_permission")
	for field, body := range map[string]map[string]any{
		"max_uses":        {"max_uses": 101},
		"max_age_seconds": {"max_age_seconds": 2592001},
	} {
		p := problemOf(t, owner.Do(http.MethodPost, "/api/v1/channels/"+ch.ID+"/invites", body), http.StatusBadRequest, "validation_failed")
		if p.Errors[0].Field != field {
			t.Fatalf("errors = %+v", p.Errors)
		}
	}
}

func TestInviteExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, _ := e.user("bob")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_age_seconds": 60})
	if _, err := e.pool.Exec(t.Context(), "UPDATE invites SET expires_at = now() - interval '1 second' WHERE code = $1", inv.Code); err != nil {
		t.Fatal(err)
	}
	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/invites/"+inv.Code, nil), http.StatusGone, "invite_expired")
	problemOf(t, join(bobC, inv.Code), http.StatusGone, "invite_expired")
	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusNotFound, "not_found")
	// The list still shows it to the managers.
	if invs := listInvites(t, owner, g); len(invs) != 1 || invs[0].Uses != 0 {
		t.Fatalf("invites = %+v", invs)
	}
}

func TestInviteConcurrentJoinMaxUsesOne(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	a, _ := e.user("bob")
	b, _ := e.user("carol")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_uses": 1})
	warmPool(t, e, 4)

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		got   [2]*testkit.Response
	)
	for i, c := range []*testkit.Client{a, b} {
		wg.Go(func() {
			<-start
			got[i] = join(c, inv.Code)
		})
	}
	close(start)
	wg.Wait()

	statuses := []int{got[0].Status, got[1].Status}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{http.StatusOK, http.StatusGone}) {
		t.Fatalf("statuses = %v: %s / %s", statuses, got[0].Body, got[1].Body)
	}
	loser := got[0]
	if loser.Status == http.StatusOK {
		loser = got[1]
	}
	problemOf(t, loser, http.StatusGone, "invite_exhausted")
	var uses, members int
	if err := e.pool.QueryRow(t.Context(), "SELECT uses FROM invites WHERE code = $1", inv.Code).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE guild_id = $1", g.ID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if uses != 1 || members != 2 {
		t.Fatalf("uses = %d, members = %d; want 1 and 2", uses, members)
	}
}

func TestJoinTwiceIsIdempotent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, _ := e.user("bob")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_uses": 1})

	expect(t, join(bobC, inv.Code), http.StatusOK)
	e.pub.reset()
	// Again, although the only use is gone: still 200, no use, no event.
	resp := join(bobC, inv.Code)
	expect(t, resp, http.StatusOK)
	var again guildBody
	resp.JSON(&again)
	if again.ID != g.ID {
		t.Fatalf("second join = %+v", again)
	}
	expect(t, join(owner, inv.Code), http.StatusOK)
	if invs := listInvites(t, owner, g); len(invs) != 1 || invs[0].Uses != 1 {
		t.Fatalf("invites = %+v", invs)
	}
	if n := len(e.pub.ofType(events.GuildMemberAdd)) + len(e.pub.ofType(events.GuildCreate)); n != 0 {
		t.Fatalf("%d join events for members", n)
	}
}

func TestDeleteAndListInvites(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, _ := e.user("bob")
	carolC, _ := e.user("carol")
	strangerC, _ := e.user("eve")
	g, ch := newGuild(t, owner, "home")
	for _, c := range []*testkit.Client{bobC, carolC} {
		expect(t, join(c, createInvite(t, owner, ch.ID, map[string]any{"max_uses": 1}).Code), http.StatusOK)
	}
	ownerInv := createInvite(t, owner, ch.ID, nil)
	bobInv := createInvite(t, bobC, ch.ID, nil)

	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/invites", nil), http.StatusForbidden, "missing_permission")
	problemOf(t, strangerC.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/invites", nil), http.StatusNotFound, "not_found")
	if invs := listInvites(t, owner, g); len(invs) != 4 || invs[0].Code != bobInv.Code {
		t.Fatalf("invites = %+v, want 4, newest first", invs)
	}

	problemOf(t, carolC.Do(http.MethodDelete, "/api/v1/invites/"+ownerInv.Code, nil), http.StatusForbidden, "missing_permission")
	problemOf(t, strangerC.Do(http.MethodDelete, "/api/v1/invites/"+ownerInv.Code, nil), http.StatusNotFound, "not_found")
	expect(t, bobC.Do(http.MethodDelete, "/api/v1/invites/"+bobInv.Code, nil), http.StatusNoContent)
	expect(t, owner.Do(http.MethodDelete, "/api/v1/invites/"+ownerInv.Code, nil), http.StatusNoContent)
	problemOf(t, owner.Do(http.MethodGet, "/api/v1/invites/"+ownerInv.Code, nil), http.StatusNotFound, "not_found")

	// Deleting the channel deletes its invites.
	last := createInvite(t, owner, ch.ID, nil)
	expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID, nil), http.StatusNoContent)
	problemOf(t, owner.Do(http.MethodGet, "/api/v1/invites/"+last.Code, nil), http.StatusNotFound, "not_found")
}

func TestInvitePagesSkipHiddenChannelsAndReachPermanentInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, u := e.user("alice")
	manager, _ := e.user("bob")
	g, visible := newGuild(t, owner, "home")
	permanent := createInvite(t, owner, visible.ID, map[string]any{"max_age_seconds": 0})
	expect(t, join(manager, permanent.Code), http.StatusOK)
	path := "/api/v1/guilds/" + g.ID
	resp := owner.Do(http.MethodPost, path+"/channels", map[string]any{"name": "hidden", "type": 0})
	expect(t, resp, http.StatusCreated)
	var hidden channelBody
	resp.JSON(&hidden)
	resp = owner.Do(http.MethodPut, "/api/v1/channels/"+hidden.ID+"/permissions/"+g.ID,
		map[string]any{"type": 0, "allow": "0", "deny": strconv.FormatUint(uint64(permissions.ViewChannel), 10)})
	expect(t, resp, http.StatusOK)
	guildID, err := snowflake.Parse(g.ID)
	if err != nil {
		t.Fatalf("parse guild ID: %v", err)
	}
	hiddenID, err := snowflake.Parse(hidden.ID)
	if err != nil {
		t.Fatalf("parse hidden channel ID: %v", err)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE roles SET permissions = permissions | $1 WHERE id = $2",
		int64(permissions.ManageGuild), guildID); err != nil {
		t.Fatalf("grant Manage Guild: %v", err)
	}
	if _, err := e.pool.Exec(t.Context(), `
		INSERT INTO invites (code, guild_id, channel_id, inviter_id, max_uses, created_at)
		SELECT lpad(n::text, 8, '0'), $1, $2, $3, 0, now() + interval '1 day'
		FROM generate_series(1, 1001) AS n`, guildID, hiddenID, u.id(t)); err != nil {
		t.Fatalf("seed hidden invites: %v", err)
	}
	createInvite(t, owner, visible.ID, nil)
	createInvite(t, owner, visible.ID, nil)
	page := func(query string) []inviteBody {
		t.Helper()
		r := manager.Do(http.MethodGet, path+"/invites"+query, nil)
		expect(t, r, http.StatusOK)
		var out []inviteBody
		r.JSON(&out)
		return out
	}
	first := page("?limit=2")
	if len(first) != 2 {
		t.Fatalf("first page length = %d, want 2", len(first))
	}
	second := page("?limit=2&before=" + first[1].Code)
	if len(second) != 1 || second[0].Code != permanent.Code || second[0].ExpiresAt != nil {
		t.Errorf("second page = %+v, want only permanent invite %s", second, permanent.Code)
	}
}

func TestInviteLookupRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *invites.Options) {
		o.LookupLimiter = ratelimit.New(rate.Every(time.Hour), 3, 2*time.Hour)
	})
	owner, _ := e.user("alice")
	bobC, _ := e.user("bob")
	for range 3 {
		problemOf(t, bobC.Do(http.MethodGet, "/api/v1/invites/aaaaaaaa", nil), http.StatusNotFound, "not_found")
	}
	resp := bobC.Do(http.MethodPost, "/api/v1/invites/aaaaaaaa", nil)
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// Per user: the owner still has its own budget.
	problemOf(t, owner.Do(http.MethodGet, "/api/v1/invites/aaaaaaaa", nil), http.StatusNotFound, "not_found")
}

func TestInviteCreationRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *invites.Options) { o.CreateLimiter = nil })
	owner, _ := e.user("alice")
	_, ch := newGuild(t, owner, "home")
	for range 20 {
		createInvite(t, owner, ch.ID, nil)
	}
	problemOf(t, owner.Do(http.MethodPost, "/api/v1/channels/"+ch.ID+"/invites", map[string]any{}),
		http.StatusTooManyRequests, "rate_limited")
}

// A member's overwrites go when they leave: rejoining does not bring back
// a private channel they were let into.
func TestRejoinDoesNotRestoreOverwrites(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_age_seconds": 0})
	expect(t, join(bobC, inv.Code), http.StatusOK)

	resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": "private", "type": 0})
	expect(t, resp, http.StatusCreated)
	var private channelBody
	resp.JSON(&private)
	view := strconv.FormatUint(uint64(permissions.ViewChannel), 10)
	expect(t, owner.Do(http.MethodPut, "/api/v1/channels/"+private.ID+"/permissions/"+g.ID,
		map[string]any{"type": 0, "deny": view}), http.StatusOK)
	expect(t, owner.Do(http.MethodPut, "/api/v1/channels/"+private.ID+"/permissions/"+bob.ID,
		map[string]any{"type": 1, "allow": view}), http.StatusOK)
	expect(t, bobC.Do(http.MethodGet, "/api/v1/channels/"+private.ID, nil), http.StatusOK)

	expect(t, bobC.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusNoContent)
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM overwrites WHERE target_id = $1", bob.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d overwrites of bob survive his leaving", n)
	}
	expect(t, join(bobC, inv.Code), http.StatusOK)
	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/channels/"+private.ID, nil), http.StatusNotFound, "not_found")
}
