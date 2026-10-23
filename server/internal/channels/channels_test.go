package channels_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

const (
	roleTarget   = 0
	memberTarget = 1
)

func TestNewGuildHasDefaultChannels(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	g := createGuild(t, c, "home")
	cs := listChannels(t, c, g)
	if len(cs) != 4 {
		t.Fatalf("channels = %+v", cs)
	}
	text, general, voice, lounge := cs[0], cs[1], cs[2], cs[3]
	for _, want := range []struct {
		got      channelBody
		name     string
		typ, pos int
		parent   *channelBody
	}{
		{text, channels.DefaultTextCategory, 4, 0, nil},
		{general, channels.DefaultTextChannel, 0, 0, &text},
		{voice, channels.DefaultVoiceCategory, 4, 1, nil},
		{lounge, channels.DefaultVoiceChannel, 2, 0, &voice},
	} {
		ch := want.got
		if ch.Name != want.name || ch.Type != want.typ || ch.Position != want.pos || ch.GuildID != g.ID ||
			ch.Topic != nil || len(ch.PermissionOverwrites) != 0 {
			t.Fatalf("channel %q = %+v", want.name, ch)
		}
		if want.parent == nil {
			if ch.ParentID != nil || ch.PermissionSynced {
				t.Fatalf("category %q = %+v", ch.Name, ch)
			}
		} else if ch.ParentID == nil || *ch.ParentID != want.parent.ID || !ch.PermissionSynced {
			t.Fatalf("channel %q = %+v, want synced in %s", ch.Name, ch, want.parent.ID)
		}
	}
}

func TestHiddenChannelIsNotFound(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	strangerC, _ := e.user("eve")
	g := createGuild(t, owner, "home")
	e.join(g, bob)
	secretRole := createRole(t, owner, g, "insiders", 0)
	secret := createChannel(t, owner, g, map[string]any{"name": "secret", "type": 0})
	expect(t, putOverwrite(owner, secret.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
	expect(t, putOverwrite(owner, secret.ID, secretRole, roleTarget, permissions.ViewChannel, 0), http.StatusOK)

	if hasChannel(listChannels(t, bobC, g), secret.ID) {
		t.Fatal("bob sees the hidden channel in the list")
	}
	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/channels/"+secret.ID, nil), http.StatusNotFound, "not_found")
	problemOf(t, bobC.Do(http.MethodPatch, "/api/v1/channels/"+secret.ID, map[string]any{"name": "x"}), http.StatusNotFound, "not_found")
	problemOf(t, bobC.Do(http.MethodDelete, "/api/v1/channels/"+secret.ID, nil), http.StatusNotFound, "not_found")
	problemOf(t, putOverwrite(bobC, secret.ID, bob.ID, memberTarget, permissions.ViewChannel, 0), http.StatusNotFound, "not_found")
	if _, _, err := e.svc.Permissions(t.Context(), bob.id(t), secret.id(t)); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("Permissions of bob = %v, want ErrNotFound", err)
	}
	// Outside the guild nothing exists at all.
	problemOf(t, strangerC.Do(http.MethodGet, "/api/v1/channels/"+secret.ID, nil), http.StatusNotFound, "not_found")
	problemOf(t, strangerC.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil), http.StatusNotFound, "not_found")
	problemOf(t, strangerC.Do(http.MethodGet, "/api/v1/channels/123", nil), http.StatusNotFound, "not_found")
	problemOf(t, strangerC.Do(http.MethodGet, "/api/v1/channels/0123", nil), http.StatusNotFound, "not_found")

	setRoles(t, owner, g, bob.ID, secretRole)
	if !hasChannel(listChannels(t, bobC, g), secret.ID) {
		t.Fatal("bob with the role does not see the channel")
	}
	if ch := getChannel(t, bobC, secret.ID); ch.Name != "secret" || len(ch.PermissionOverwrites) != 2 {
		t.Fatalf("channel = %+v", ch)
	}
	p, gid, err := e.svc.Permissions(t.Context(), bob.id(t), secret.id(t))
	if err != nil || gid != g.id(t) || !p.Has(permissions.ViewChannel|permissions.SendMessages) {
		t.Fatalf("Permissions of bob = %v, %v, %v", p, gid, err)
	}
}

func TestCategorySyncInheritsOverwrites(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g := createGuild(t, owner, "home")
	e.join(g, bob)
	cs := listChannels(t, owner, g)
	cat, general := byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultTextChannel)

	// A deny on the category hides its synced channel too.
	expect(t, putOverwrite(owner, cat.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
	seen := listChannels(t, bobC, g)
	if hasChannel(seen, cat.ID) || hasChannel(seen, general.ID) {
		t.Fatalf("bob sees %+v", seen)
	}
	problemOf(t, bobC.Do(http.MethodGet, "/api/v1/channels/"+general.ID, nil), http.StatusNotFound, "not_found")
	got := getChannel(t, owner, general.ID)
	if !got.PermissionSynced || len(got.PermissionOverwrites) != 1 || got.PermissionOverwrites[0].ID != g.ID ||
		got.PermissionOverwrites[0].Deny != perm(permissions.ViewChannel) {
		t.Fatalf("synced channel = %+v", got)
	}

	// Changing an overwrite of the channel ends the sync: it keeps the
	// category's overwrites as its own, plus the change.
	resp := putOverwrite(owner, general.ID, bob.ID, memberTarget, permissions.ViewChannel, 0)
	expect(t, resp, http.StatusOK)
	resp.JSON(&got)
	if got.PermissionSynced || len(got.PermissionOverwrites) != 2 {
		t.Fatalf("after own overwrite = %+v", got)
	}
	seen = listChannels(t, bobC, g)
	if !hasChannel(seen, general.ID) || hasChannel(seen, cat.ID) {
		t.Fatalf("bob sees %+v, want the channel without its category", seen)
	}
	// Later category changes no longer reach it.
	expect(t, putOverwrite(owner, cat.ID, bob.ID, memberTarget, 0, permissions.SendMessages), http.StatusOK)
	bobAllow := overwriteBody{ID: bob.ID, Type: memberTarget, Allow: perm(permissions.ViewChannel), Deny: "0"}
	if got = getChannel(t, owner, general.ID); len(got.PermissionOverwrites) != 2 ||
		!slices.Contains(got.PermissionOverwrites, bobAllow) {
		t.Fatalf("unsynced channel follows the category: %+v", got)
	}

	// sync_permissions brings the category's overwrites back.
	resp = owner.Do(http.MethodPatch, "/api/v1/channels/"+general.ID, map[string]any{"sync_permissions": true})
	expect(t, resp, http.StatusOK)
	resp.JSON(&got)
	catNow := getChannel(t, owner, cat.ID)
	if !got.PermissionSynced || !slices.Equal(got.PermissionOverwrites, catNow.PermissionOverwrites) {
		t.Fatalf("after sync = %+v, category = %+v", got, catNow)
	}
	if hasChannel(listChannels(t, bobC, g), general.ID) {
		t.Fatal("bob still sees the re-synced channel")
	}

	// A top-level channel has nothing to sync with.
	top := createChannel(t, owner, g, map[string]any{"name": "top", "type": 0})
	if top.PermissionSynced {
		t.Fatalf("top-level channel = %+v", top)
	}
	p := problemOf(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+top.ID, map[string]any{"sync_permissions": true}),
		http.StatusBadRequest, "validation_failed")
	if p.Errors[0].Field != "sync_permissions" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestOverwriteCannotGrantMissingPermission(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	modC, mod := e.user("mod")
	plainC, plain := e.user("plain")
	_, eve := e.user("eve")
	g := createGuild(t, owner, "home")
	e.join(g, mod)
	e.join(g, plain)
	// Roles go in right above @everyone, so create the lower one last.
	modRole := createRole(t, owner, g, "mods", permissions.ManageRoles)
	low := createRole(t, owner, g, "low", 0)
	setRoles(t, owner, g, mod.ID, modRole)
	ch := createChannel(t, owner, g, map[string]any{"name": "room", "type": 0})

	// Bits the moderator holds can be granted and denied, others not.
	expect(t, putOverwrite(modC, ch.ID, low, roleTarget, permissions.SendMessages, 0), http.StatusOK)
	problemOf(t, putOverwrite(modC, ch.ID, low, roleTarget, permissions.ManageChannels, 0), http.StatusForbidden, "missing_permission")
	problemOf(t, putOverwrite(modC, ch.ID, low, roleTarget, 0, permissions.ManageChannels), http.StatusForbidden, "missing_permission")
	// Not on its own role or above, but on @everyone.
	problemOf(t, putOverwrite(modC, ch.ID, modRole, roleTarget, permissions.SendMessages, 0), http.StatusForbidden, "missing_permission")
	expect(t, putOverwrite(modC, ch.ID, g.ID, roleTarget, 0, permissions.AttachFiles), http.StatusOK)
	// Unchanged bits are not "granted": re-sending an overwrite the owner
	// set with a bit the moderator lacks works as long as that bit stays.
	expect(t, putOverwrite(owner, ch.ID, plain.ID, memberTarget, permissions.ManageChannels, 0), http.StatusOK)
	expect(t, putOverwrite(modC, ch.ID, plain.ID, memberTarget, permissions.ManageChannels|permissions.SendMessages, 0), http.StatusOK)
	problemOf(t, modC.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID+"/permissions/"+plain.ID, nil), http.StatusForbidden, "missing_permission")
	expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID+"/permissions/"+plain.ID, nil), http.StatusNoContent)
	// Removing one that is not there is a no-op.
	expect(t, modC.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID+"/permissions/"+plain.ID, nil), http.StatusNoContent)

	// Without MANAGE_ROLES nothing at all.
	problemOf(t, putOverwrite(plainC, ch.ID, low, roleTarget, 0, 0), http.StatusForbidden, "missing_permission")
	// Targets must be in the guild.
	problemOf(t, putOverwrite(modC, ch.ID, eve.ID, memberTarget, permissions.SendMessages, 0), http.StatusNotFound, "not_found")
	problemOf(t, putOverwrite(modC, ch.ID, eve.ID, roleTarget, permissions.SendMessages, 0), http.StatusNotFound, "not_found")

	// ADMINISTRATOR, unknown bits and contradictions are refused, even for
	// the owner.
	for name, body := range map[string]map[string]any{
		"admin allow":   {"type": 0, "allow": perm(permissions.Administrator), "deny": "0"},
		"admin deny":    {"type": 0, "allow": "0", "deny": perm(permissions.Administrator)},
		"unknown bit":   {"type": 0, "allow": perm(1 << 40), "deny": "0"},
		"both":          {"type": 0, "allow": perm(permissions.SendMessages), "deny": perm(permissions.SendMessages)},
		"bad type":      {"type": 7, "allow": "0", "deny": "0"},
		"type required": {"allow": "0"},
	} {
		resp := owner.Do(http.MethodPut, "/api/v1/channels/"+ch.ID+"/permissions/"+low, body)
		if resp.Status != http.StatusBadRequest || !strings.Contains(string(resp.Body), "validation_failed") {
			t.Fatalf("%s: %d %s", name, resp.Status, resp.Body)
		}
	}
	got := getChannel(t, owner, ch.ID)
	if len(got.PermissionOverwrites) != 2 {
		t.Fatalf("overwrites = %+v", got.PermissionOverwrites)
	}
}

func TestParentMustBeCategoryOfSameGuild(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "one")
	other := createGuild(t, owner, "two")
	cs := listChannels(t, owner, g)
	cat, general := byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultTextChannel)
	foreignCat := byName(t, listChannels(t, owner, other), channels.DefaultTextCategory)

	check := func(name string, status int, body []byte, field string) {
		t.Helper()
		if status != http.StatusBadRequest || !strings.Contains(string(body), `"field":"`+field+`"`) {
			t.Fatalf("%s: %d %s", name, status, body)
		}
	}
	for name, parent := range map[string]string{"text channel": general.ID, "other guild": foreignCat.ID, "nothing": "42"} {
		resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": "x", "type": 0, "parent_id": parent})
		check("create under "+name, resp.Status, resp.Body, "parent_id")
		resp = owner.Do(http.MethodPatch, "/api/v1/channels/"+general.ID, map[string]any{"parent_id": parent})
		check("move under "+name, resp.Status, resp.Body, "parent_id")
		resp = owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/channels", []map[string]any{{"id": general.ID, "position": 0, "parent_id": parent}})
		check("reorder under "+name, resp.Status, resp.Body, "channels")
	}
	resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": "x", "type": 4, "parent_id": cat.ID})
	check("category in category", resp.Status, resp.Body, "parent_id")
	resp = owner.Do(http.MethodPatch, "/api/v1/channels/"+byName(t, cs, channels.DefaultVoiceCategory).ID, map[string]any{"parent_id": cat.ID})
	check("move category into category", resp.Status, resp.Body, "parent_id")

	// A real category of the guild works: the channel goes last, synced.
	ch := createChannel(t, owner, g, map[string]any{"name": "news", "type": 0, "parent_id": cat.ID, "topic": "  daily  "})
	if ch.ParentID == nil || *ch.ParentID != cat.ID || ch.Position != 1 || !ch.PermissionSynced ||
		ch.Topic == nil || *ch.Topic != "daily" {
		t.Fatalf("channel = %+v", ch)
	}
	// And leaving it for the top level puts it last there, unsynced.
	resp = owner.Do(http.MethodPatch, "/api/v1/channels/"+ch.ID, map[string]any{"parent_id": nil, "topic": nil})
	expect(t, resp, http.StatusOK)
	resp.JSON(&ch)
	if ch.ParentID != nil || ch.Position != 2 || ch.PermissionSynced || ch.Topic != nil {
		t.Fatalf("moved channel = %+v", ch)
	}
}

func TestCreateChannelValidation(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	strangerC, _ := e.user("eve")
	g := createGuild(t, owner, "home")
	e.join(g, bob)
	path := "/api/v1/guilds/" + g.ID + "/channels"
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"no name":       {map[string]any{"type": 0}, "name"},
		"no type":       {map[string]any{"name": "x"}, "type"},
		"bad type":      {map[string]any{"name": "x", "type": 1}, "type"},
		"invisible":     {map[string]any{"name": string(rune(0x200B)) + " ", "type": 0}, "name"},
		"bidi":          {map[string]any{"name": "a" + string(rune(0x202E)) + "b", "type": 0}, "name"},
		"long name":     {map[string]any{"name": strings.Repeat("я", 101), "type": 0}, "name"},
		"long topic":    {map[string]any{"name": "x", "type": 0, "topic": strings.Repeat("я", 1025)}, "topic"},
		"bidi topic":    {map[string]any{"name": "x", "type": 0, "topic": "a" + string(rune(0x2066))}, "topic"},
		"voice topic":   {map[string]any{"name": "x", "type": 2, "topic": "hi"}, "topic"},
		"control topic": {map[string]any{"name": "x", "type": 0, "topic": "a" + string(rune(7))}, "topic"},
	} {
		p := problemOf(t, owner.Do(http.MethodPost, path, tc.body), http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != tc.field {
			t.Fatalf("%s: errors = %+v", name, p.Errors)
		}
	}
	ch := createChannel(t, owner, g, map[string]any{"name": "x", "type": 0, "topic": strings.Repeat("я", 1024)})
	if ch.Topic == nil {
		t.Fatal("topic dropped")
	}
	ch = createChannel(t, owner, g, map[string]any{"name": "y", "type": 0, "topic": "   "})
	if ch.Topic != nil {
		t.Fatalf("blank topic = %q", *ch.Topic)
	}
	problemOf(t, bobC.Do(http.MethodPost, path, map[string]any{"name": "x", "type": 0}), http.StatusForbidden, "missing_permission")
	problemOf(t, strangerC.Do(http.MethodPost, path, map[string]any{"name": "x", "type": 0}), http.StatusNotFound, "not_found")
}

func TestDeleteCategoryKeepsChildren(t *testing.T) {
	e := newEnv(t)
	owner, alice := e.user("alice")
	bobC, bob := e.user("bob")
	g := createGuild(t, owner, "home")
	e.join(g, bob)
	cs := listChannels(t, owner, g)
	cat, general := byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultTextChannel)
	voiceCat := byName(t, cs, channels.DefaultVoiceCategory)
	expect(t, putOverwrite(owner, cat.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
	problemOf(t, bobC.Do(http.MethodDelete, "/api/v1/channels/"+voiceCat.ID, nil), http.StatusForbidden, "missing_permission")
	e.pub.reset()

	expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+cat.ID, nil), http.StatusNoContent)
	problemOf(t, owner.Do(http.MethodGet, "/api/v1/channels/"+cat.ID, nil), http.StatusNotFound, "not_found")
	cs = listChannels(t, owner, g)
	if len(cs) != 3 {
		t.Fatalf("channels = %+v", cs)
	}
	moved := byName(t, cs, channels.DefaultTextChannel)
	if moved.ID != general.ID || moved.ParentID != nil || moved.PermissionSynced || moved.Position != 1 ||
		len(moved.PermissionOverwrites) != 1 {
		t.Fatalf("child after deleting its category = %+v", moved)
	}
	if v := byName(t, cs, channels.DefaultVoiceCategory); v.Position != 0 {
		t.Fatalf("voice category = %+v, want position 0", v)
	}
	// It kept the category's overwrites, so it is still hidden.
	if hasChannel(listChannels(t, bobC, g), general.ID) {
		t.Fatal("bob sees the former child")
	}

	// CHANNEL_DELETE went to those who could see the category before.
	del := e.pub.ofType(events.ChannelDelete)
	if len(del) != 1 || del[0].Audience.Kind != events.AudienceUsers ||
		!slices.Equal(del[0].Audience.UserIDs, []snowflake.ID{alice.id(t)}) {
		t.Fatalf("CHANNEL_DELETE = %+v", del)
	}
	if len(e.pub.ofType(events.ChannelUpdate)) == 0 || len(e.pub.ofType(events.GuildSync)) != 1 {
		t.Fatalf("events = %+v", e.pub.events)
	}

	// A visible channel's deletion reaches everyone who saw it.
	e.pub.reset()
	expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+voiceCat.ID, nil), http.StatusNoContent)
	del = e.pub.ofType(events.ChannelDelete)
	if len(del) != 1 || len(del[0].Audience.UserIDs) != 2 {
		t.Fatalf("CHANNEL_DELETE = %+v", del)
	}
}

func TestReorderChannels(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g := createGuild(t, owner, "home")
	e.join(g, bob)
	cs := listChannels(t, owner, g)
	textCat, voiceCat := byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultVoiceCategory)
	general := byName(t, cs, channels.DefaultTextChannel)
	a := createChannel(t, owner, g, map[string]any{"name": "a", "type": 0, "parent_id": textCat.ID})
	b := createChannel(t, owner, g, map[string]any{"name": "b", "type": 0, "parent_id": textCat.ID})
	path := "/api/v1/guilds/" + g.ID + "/channels"

	type place struct {
		parent string
		pos    int
	}
	where := func(cs []channelBody) map[string]place {
		out := make(map[string]place, len(cs))
		for _, c := range cs {
			p := place{pos: c.Position}
			if c.ParentID != nil {
				p.parent = *c.ParentID
			}
			out[c.Name] = p
		}
		return out
	}
	reorder := func(moves ...map[string]any) map[string]place {
		t.Helper()
		resp := owner.Do(http.MethodPatch, path, moves)
		expect(t, resp, http.StatusOK)
		var got []channelBody
		resp.JSON(&got)
		return where(got)
	}

	got := reorder(map[string]any{"id": b.ID, "position": 0})
	if got["b"] != (place{textCat.ID, 0}) || got[general.Name] != (place{textCat.ID, 1}) || got["a"] != (place{textCat.ID, 2}) {
		t.Fatalf("after moving b first: %+v", got)
	}
	got = reorder(map[string]any{"id": a.ID, "position": 0, "parent_id": voiceCat.ID},
		map[string]any{"id": general.ID, "position": 0, "parent_id": nil})
	want := map[string]place{
		general.Name: {"", 0}, textCat.Name: {"", 1}, voiceCat.Name: {"", 2},
		"b": {textCat.ID, 0}, "a": {voiceCat.ID, 0}, channels.DefaultVoiceChannel: {voiceCat.ID, 1},
	}
	for name, p := range want {
		if got[name] != p {
			t.Fatalf("%s at %+v, want %+v (all: %+v)", name, got[name], p, got)
		}
	}
	// The order is what a fresh list shows; every channel that changed
	// parent is unsynced, one that did not stays synced.
	cs = listChannels(t, owner, g)
	if !sameMap(where(cs), want) {
		t.Fatalf("list = %+v", where(cs))
	}
	if byName(t, cs, general.Name).PermissionSynced || byName(t, cs, "a").PermissionSynced ||
		!byName(t, cs, "b").PermissionSynced {
		t.Fatalf("sync flags: %+v", cs)
	}

	for name, moves := range map[string][]map[string]any{
		"twice":       {{"id": a.ID, "position": 0}, {"id": a.ID, "position": 1}},
		"negative":    {{"id": a.ID, "position": -1}},
		"unknown":     {{"id": "42", "position": 0}},
		"same slot":   {{"id": a.ID, "position": 0}, {"id": b.ID, "position": 0, "parent_id": voiceCat.ID}},
		"no position": {{"id": a.ID}},
	} {
		if resp := owner.Do(http.MethodPatch, path, moves); resp.Status != http.StatusBadRequest || !strings.Contains(string(resp.Body), "validation_failed") {
			t.Fatalf("%s: %d %s", name, resp.Status, resp.Body)
		}
	}
	problemOf(t, bobC.Do(http.MethodPatch, path, []map[string]any{{"id": a.ID, "position": 1}}), http.StatusForbidden, "missing_permission")
}

func sameMap[K comparable, V comparable](a, b map[K]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestViewersRespectsOverwrites(t *testing.T) {
	e := newEnv(t)
	owner, alice := e.user("alice")
	_, bob := e.user("bob")
	_, carol := e.user("carol")
	_, dave := e.user("dave")
	g := createGuild(t, owner, "home")
	for _, u := range []userBody{bob, carol, dave} {
		e.join(g, u)
	}
	admin := createRole(t, owner, g, "admins", permissions.Administrator)
	crew := createRole(t, owner, g, "crew", 0)
	setRoles(t, owner, g, bob.ID, crew)
	setRoles(t, owner, g, dave.ID, admin)
	ch := createChannel(t, owner, g, map[string]any{"name": "crew", "type": 0})
	viewers := func() []snowflake.ID {
		t.Helper()
		ids, err := e.svc.Viewers(t.Context(), ch.id(t))
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	ids := func(us ...userBody) []snowflake.ID {
		out := make([]snowflake.ID, len(us))
		for i, u := range us {
			out[i] = u.id(t)
		}
		slices.Sort(out)
		return out
	}
	if got := viewers(); !slices.Equal(got, ids(alice, bob, carol, dave)) {
		t.Fatalf("open channel viewers = %v", got)
	}
	expect(t, putOverwrite(owner, ch.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
	expect(t, putOverwrite(owner, ch.ID, crew, roleTarget, permissions.ViewChannel, 0), http.StatusOK)
	// The owner and administrators always see it, the crew role through
	// its allow.
	if got := viewers(); !slices.Equal(got, ids(alice, bob, dave)) {
		t.Fatalf("viewers = %v", got)
	}
	expect(t, putOverwrite(owner, ch.ID, carol.ID, memberTarget, permissions.ViewChannel, 0), http.StatusOK)
	expect(t, putOverwrite(owner, ch.ID, bob.ID, memberTarget, 0, permissions.ViewChannel), http.StatusOK)
	expect(t, putOverwrite(owner, ch.ID, dave.ID, memberTarget, 0, permissions.ViewChannel), http.StatusOK)
	if got := viewers(); !slices.Equal(got, ids(alice, carol, dave)) {
		t.Fatalf("viewers with member overwrites = %v", got)
	}
	if _, err := e.svc.Viewers(t.Context(), 42); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("viewers of a missing channel: %v", err)
	}
}

func TestChannelEventsPublished(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "home")
	e.pub.reset()
	ch := createChannel(t, owner, g, map[string]any{"name": "news", "type": 0})
	created := e.pub.ofType(events.ChannelCreate)
	if len(created) != 1 || created[0].Audience.Kind != events.AudienceChannel ||
		created[0].Audience.ChannelID != ch.id(t) || created[0].Audience.GuildID != g.id(t) {
		t.Fatalf("CHANNEL_CREATE = %+v", created)
	}
	expect(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+ch.ID, map[string]any{"name": "updates"}), http.StatusOK)
	if up := e.pub.ofType(events.ChannelUpdate); len(up) != 1 || up[0].Payload.(channels.PublicChannel).Name != "updates" {
		t.Fatalf("CHANNEL_UPDATE = %+v", up)
	}
	if len(e.pub.ofType(events.GuildSync)) != 0 {
		t.Fatal("a rename published GUILD_SYNC")
	}
	expect(t, putOverwrite(owner, ch.ID, g.ID, roleTarget, 0, permissions.SendMessages), http.StatusOK)
	if len(e.pub.ofType(events.ChannelUpdate)) != 2 || len(e.pub.ofType(events.GuildSync)) != 1 {
		t.Fatalf("events after an overwrite = %+v", e.pub.events)
	}
}

func TestCategoryOverwriteUpdatesSyncedChannels(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "home")
	cs := listChannels(t, owner, g)
	cat, general := byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultTextChannel)
	e.pub.reset()
	expect(t, putOverwrite(owner, cat.ID, g.ID, roleTarget, 0, permissions.SendMessages), http.StatusOK)
	var ids []string
	for _, ev := range e.pub.ofType(events.ChannelUpdate) {
		c := ev.Payload.(channels.PublicChannel)
		ids = append(ids, c.ID.String())
		if len(c.PermissionOverwrites) != 1 {
			t.Fatalf("CHANNEL_UPDATE %s overwrites = %+v", c.Name, c.PermissionOverwrites)
		}
	}
	slices.Sort(ids)
	want := []string{cat.ID, general.ID}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Fatalf("CHANNEL_UPDATE for %v, want %v", ids, want)
	}
}

func TestDeleteGuildWithChannels(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "home")
	cs := listChannels(t, owner, g)
	createChannel(t, owner, g, map[string]any{"name": "x", "type": 0, "parent_id": cs[0].ID})
	expect(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID, nil), http.StatusNoContent)
	for _, c := range cs {
		if _, err := e.svc.Lookup(t.Context(), c.id(t)); !errors.Is(err, channels.ErrNotFound) {
			t.Fatalf("channel %s after guild deletion: %v", c.Name, err)
		}
	}
}

func TestDeleteRoleDropsItsOverwrites(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	g := createGuild(t, owner, "home")
	keep := createRole(t, owner, g, "keep", 0)
	gone := createRole(t, owner, g, "gone", 0)
	ch := createChannel(t, owner, g, map[string]any{"name": "room", "type": 0})
	expect(t, putOverwrite(owner, ch.ID, gone, roleTarget, permissions.SendMessages, 0), http.StatusOK)
	expect(t, putOverwrite(owner, ch.ID, keep, roleTarget, permissions.SendMessages, 0), http.StatusOK)
	expect(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/roles/"+gone, nil), http.StatusNoContent)
	got := getChannel(t, owner, ch.ID)
	if len(got.PermissionOverwrites) != 1 || got.PermissionOverwrites[0].ID != keep {
		t.Fatalf("overwrites after deleting a role = %+v", got.PermissionOverwrites)
	}
}

// moverEnv is a guild whose text category is private (the mover's role may
// see it) and whose voice category is public; the mover has
// MANAGE_CHANNELS but not MANAGE_ROLES.
func moverEnv(t *testing.T) (e *env, owner, mover, bobC *testkit.Client, g guildBody, private, public channelBody) {
	t.Helper()
	e = newEnv(t)
	owner, _ = e.user("alice")
	mover, m := e.user("mover")
	bobC, bob := e.user("bob")
	g = createGuild(t, owner, "home")
	e.join(g, m)
	e.join(g, bob)
	movers := createRole(t, owner, g, "movers", permissions.ManageChannels)
	setRoles(t, owner, g, m.ID, movers)
	cs := listChannels(t, owner, g)
	private, public = byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultVoiceCategory)
	expect(t, putOverwrite(owner, private.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
	expect(t, putOverwrite(owner, private.ID, movers, roleTarget, permissions.ViewChannel, 0), http.StatusOK)
	return e, owner, mover, bobC, g, private, public
}

func TestMoveKeepsVisibility(t *testing.T) {
	_, owner, mover, bobC, g, private, public := moverEnv(t)
	secret := byName(t, listChannels(t, owner, g), channels.DefaultTextChannel)
	open := byName(t, listChannels(t, owner, g), channels.DefaultVoiceChannel)

	// Private to public with PATCH: still hidden, now with its own copy.
	resp := mover.Do(http.MethodPatch, "/api/v1/channels/"+secret.ID, map[string]any{"parent_id": public.ID})
	expect(t, resp, http.StatusOK)
	var got channelBody
	resp.JSON(&got)
	if got.PermissionSynced || got.ParentID == nil || *got.ParentID != public.ID || len(got.PermissionOverwrites) != 2 {
		t.Fatalf("moved private channel = %+v", got)
	}
	if hasChannel(listChannels(t, bobC, g), secret.ID) {
		t.Fatal("moving to a public category revealed the channel")
	}
	// Public to private with a reorder: still visible.
	resp = mover.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/channels",
		[]map[string]any{{"id": open.ID, "position": 0, "parent_id": private.ID}})
	expect(t, resp, http.StatusOK)
	if got = getChannel(t, owner, open.ID); got.PermissionSynced || len(got.PermissionOverwrites) != 0 {
		t.Fatalf("moved public channel = %+v", got)
	}
	if !hasChannel(listChannels(t, bobC, g), open.ID) {
		t.Fatal("moving to a private category hid the channel")
	}
	// Re-syncing is a permission change the mover may not make.
	problemOf(t, mover.Do(http.MethodPatch, "/api/v1/channels/"+open.ID, map[string]any{"sync_permissions": true}),
		http.StatusForbidden, "missing_permission")
	expect(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+open.ID, map[string]any{"sync_permissions": true}), http.StatusOK)
	if hasChannel(listChannels(t, bobC, g), open.ID) {
		t.Fatal("bob sees the channel synced with the private category")
	}
}

func TestSyncRequiresOverwriteAuthorization(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("alice")
	modC, mod := e.user("mod")
	_, bob := e.user("bob")
	g := createGuild(t, owner, "home")
	e.join(g, mod)
	e.join(g, bob)
	// Created top-down: boss ends up above mods, low below.
	boss := createRole(t, owner, g, "boss", 0)
	mods := createRole(t, owner, g, "mods", permissions.ManageChannels|permissions.ManageRoles)
	low := createRole(t, owner, g, "low", 0)
	setRoles(t, owner, g, mod.ID, mods)
	cs := listChannels(t, owner, g)
	cat, general := byName(t, cs, channels.DefaultTextCategory), byName(t, cs, channels.DefaultTextChannel)
	// Unsync the channel with an overwrite of its own.
	expect(t, putOverwrite(owner, general.ID, bob.ID, memberTarget, permissions.AttachFiles, 0), http.StatusOK)
	sync := func(c *testkit.Client) *testkit.Response {
		return c.Do(http.MethodPatch, "/api/v1/channels/"+general.ID, map[string]any{"sync_permissions": true})
	}
	catOW := func(target string, allow permissions.Permission) {
		t.Helper()
		expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+cat.ID+"/permissions/"+boss, nil), http.StatusNoContent)
		expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+cat.ID+"/permissions/"+low, nil), http.StatusNoContent)
		expect(t, putOverwrite(owner, cat.ID, target, roleTarget, allow, 0), http.StatusOK)
	}

	// Syncing would put an overwrite on a role above the moderator.
	catOW(boss, permissions.SendMessages)
	problemOf(t, sync(modC), http.StatusForbidden, "missing_permission")
	// Syncing would grant a bit the moderator lacks.
	catOW(low, permissions.ManageGuild)
	problemOf(t, sync(modC), http.StatusForbidden, "missing_permission")
	if getChannel(t, owner, general.ID).PermissionSynced {
		t.Fatal("a refused sync took effect")
	}
	// Within the moderator's rights it works (dropping bob's overwrite,
	// a member target, needs no rank).
	catOW(low, permissions.SendMessages)
	resp := sync(modC)
	expect(t, resp, http.StatusOK)
	var got channelBody
	resp.JSON(&got)
	if !got.PermissionSynced || len(got.PermissionOverwrites) != 1 || got.PermissionOverwrites[0].ID != low {
		t.Fatalf("after sync = %+v", got)
	}
	// The owner may sync anything.
	expect(t, putOverwrite(owner, general.ID, bob.ID, memberTarget, permissions.AttachFiles, 0), http.StatusOK)
	catOW(boss, permissions.ManageGuild)
	expect(t, sync(owner), http.StatusOK)
}
