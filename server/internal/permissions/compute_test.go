package permissions_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	guildID snowflake.ID = 1000
	ownerID snowflake.ID = 1
	userID  snowflake.ID = 2
	otherID snowflake.ID = 3
	roleA   snowflake.ID = 11
	roleB   snowflake.ID = 12
	roleFar snowflake.ID = 13
)

func everyone(p permissions.Permission) permissions.Role {
	return permissions.Role{ID: guildID, Position: 0, Permissions: p}
}

func subject(everyonePerms permissions.Permission, roles ...permissions.Role) permissions.Subject {
	return permissions.Subject{
		UserID: userID, OwnerID: ownerID, GuildID: guildID,
		Everyone: everyone(everyonePerms), Roles: roles,
	}
}

func role(id snowflake.ID, pos int, p permissions.Permission) permissions.Role {
	return permissions.Role{ID: id, Position: pos, Permissions: p}
}

func roleOW(id snowflake.ID, allow, deny permissions.Permission) permissions.Overwrite {
	return permissions.Overwrite{ID: id, Type: permissions.OverwriteRole, Allow: allow, Deny: deny}
}

func memberOW(id snowflake.ID, allow, deny permissions.Permission) permissions.Overwrite {
	return permissions.Overwrite{ID: id, Type: permissions.OverwriteMember, Allow: allow, Deny: deny}
}

func TestBase(t *testing.T) {
	t.Parallel()
	const (
		view = permissions.ViewChannel
		send = permissions.SendMessages
		adm  = permissions.Administrator
	)
	owner := subject(0)
	owner.UserID = ownerID

	tests := []struct {
		name string
		s    permissions.Subject
		want permissions.Permission
	}{
		{"owner without roles gets all", owner, permissions.All},
		{"no roles gives everyone", subject(view | send), view | send},
		{"roles are unioned with everyone", subject(view, role(roleA, 1, send), role(roleB, 2, permissions.AttachFiles)), view | send | permissions.AttachFiles},
		{"administrator role gives all", subject(view, role(roleA, 1, adm)), permissions.All},
		{"administrator on everyone gives all", subject(adm), permissions.All},
		{"empty subject has nothing", subject(0), 0},
		{"unknown bits are preserved", subject(1 << 40), 1 << 40},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := permissions.Base(tc.s); got != tc.want {
				t.Fatalf("Base = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestInChannel(t *testing.T) {
	t.Parallel()
	const (
		view = permissions.ViewChannel
		send = permissions.SendMessages
		hist = permissions.ReadMessageHistory
		att  = permissions.AttachFiles
		adm  = permissions.Administrator
	)
	owner := subject(0)
	owner.UserID = ownerID
	member := subject(view|send|hist, role(roleA, 1, 0), role(roleB, 2, 0))

	tests := []struct {
		name string
		s    permissions.Subject
		ow   []permissions.Overwrite
		want permissions.Permission
	}{
		{"nil overwrites give base", member, nil, view | send | hist},
		{"empty overwrites give base", member, []permissions.Overwrite{}, view | send | hist},
		{"owner ignores deny", owner, []permissions.Overwrite{roleOW(guildID, 0, permissions.All)}, permissions.All},
		{"administrator role ignores deny", subject(view, role(roleA, 1, adm)),
			[]permissions.Overwrite{roleOW(guildID, 0, view), memberOW(userID, 0, view)}, permissions.All},
		{"everyone deny removes send", member,
			[]permissions.Overwrite{roleOW(guildID, 0, send)}, view | hist},
		{"everyone allow adds attach", member,
			[]permissions.Overwrite{roleOW(guildID, att, 0)}, view | send | hist | att},
		{"everyone: deny applied before allow", member,
			[]permissions.Overwrite{roleOW(guildID, send, send)}, view | send | hist},
		{"deny on everyone, allow on role wins", member,
			[]permissions.Overwrite{roleOW(guildID, 0, send), roleOW(roleA, send, 0)}, view | send | hist},
		{"deny on one role, allow on another role of member: allow wins", member,
			[]permissions.Overwrite{roleOW(roleA, 0, send), roleOW(roleB, send, 0)}, view | send | hist},
		{"role deny alone removes", member,
			[]permissions.Overwrite{roleOW(roleA, 0, send)}, view | hist},
		{"allow on role, deny on member: denied", member,
			[]permissions.Overwrite{roleOW(roleA, att, 0), memberOW(userID, 0, att)}, view | send | hist},
		{"member allow beats role deny", member,
			[]permissions.Overwrite{roleOW(roleA, 0, send), memberOW(userID, send, 0)}, view | send | hist},
		{"member: deny applied before allow", member,
			[]permissions.Overwrite{memberOW(userID, send, send)}, view | send | hist},
		{"deny view removes everything", member,
			[]permissions.Overwrite{roleOW(guildID, 0, view)}, 0},
		{"member deny view removes everything", member,
			[]permissions.Overwrite{memberOW(userID, att, view)}, 0},
		{"allow cannot resurrect when view is missing", subject(send),
			[]permissions.Overwrite{roleOW(guildID, att, 0)}, 0},
		{"foreign role overwrite is ignored", member,
			[]permissions.Overwrite{roleOW(roleFar, 0, view)}, view | send | hist},
		{"foreign member overwrite is ignored", member,
			[]permissions.Overwrite{memberOW(otherID, 0, view)}, view | send | hist},
		{"role-typed overwrite with the user id is ignored", member,
			[]permissions.Overwrite{roleOW(userID, 0, view)}, view | send | hist},
		{"member-typed overwrite with a role id is ignored", member,
			[]permissions.Overwrite{memberOW(roleA, 0, view)}, view | send | hist},
		{"member-typed overwrite with the guild id is ignored", member,
			[]permissions.Overwrite{memberOW(guildID, 0, view)}, view | send | hist},
		{"two roles deny unioned", member,
			[]permissions.Overwrite{roleOW(roleA, 0, send), roleOW(roleB, 0, hist)}, view},
		{"order of overwrites does not matter", member,
			[]permissions.Overwrite{memberOW(userID, att, 0), roleOW(roleB, 0, send), roleOW(guildID, 0, hist)}, view | att},
		{"view granted by overwrite on role", subject(send, role(roleA, 1, 0)),
			[]permissions.Overwrite{roleOW(roleA, view, 0)}, send | view},
		{"view granted by member overwrite", subject(0),
			[]permissions.Overwrite{memberOW(userID, view|send, 0)}, view | send},
		{"unknown bits survive", subject(view | 1<<40),
			[]permissions.Overwrite{roleOW(guildID, 1<<41, 0)}, view | 1<<40 | 1<<41},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := permissions.InChannel(tc.s, tc.ow); got != tc.want {
				t.Fatalf("InChannel = %#x, want %#x", uint64(got), uint64(tc.want))
			}
		})
	}
}

// Two overwrites for the same target is a programming error upstream; the
// documented behaviour is that only the first one counts.
func TestInChannelDuplicateOverwritesFirstWins(t *testing.T) {
	t.Parallel()
	const (
		view = permissions.ViewChannel
		send = permissions.SendMessages
	)
	s := subject(view|send, role(roleA, 1, 0))
	tests := []struct {
		name string
		ow   []permissions.Overwrite
	}{
		{"everyone", []permissions.Overwrite{roleOW(guildID, 0, send), roleOW(guildID, 0, view)}},
		{"role", []permissions.Overwrite{roleOW(roleA, 0, send), roleOW(roleA, 0, view)}},
		{"member", []permissions.Overwrite{memberOW(userID, 0, send), memberOW(userID, 0, view)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := permissions.InChannel(s, tc.ow[:1])
			if first != view {
				t.Fatalf("sanity: first-only result = %#x, want view only", uint64(first))
			}
			if got := permissions.InChannel(s, tc.ow); got != first {
				t.Fatalf("InChannel with duplicates = %#x, want first-only %#x", uint64(got), uint64(first))
			}
		})
	}
}

func TestHierarchy(t *testing.T) {
	t.Parallel()
	owner := subject(0)
	owner.UserID = ownerID
	admin := subject(0, role(roleA, 5, permissions.Administrator))
	manager := subject(0, role(roleA, 5, permissions.ManageRoles))
	plain := subject(permissions.ViewChannel, role(roleB, 7, 0))
	lowPlain := subject(0)

	t.Run("HighestPosition", func(t *testing.T) {
		t.Parallel()
		if got := permissions.HighestPosition(owner); got <= 1<<30 {
			t.Fatalf("owner position = %d, want max", got)
		}
		if got := permissions.HighestPosition(lowPlain); got != 0 {
			t.Fatalf("no roles position = %d, want 0", got)
		}
		s := subject(0, role(roleA, 3, 0), role(roleB, 9, 0))
		if got := permissions.HighestPosition(s); got != 9 {
			t.Fatalf("position = %d, want 9", got)
		}
	})

	t.Run("CanManageRole", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name   string
			actor  permissions.Subject
			target permissions.Role
			want   bool
		}{
			{"owner manages any", owner, role(roleFar, 1000, 0), true},
			{"manager manages lower role", manager, role(roleFar, 4, 0), true},
			{"manager cannot manage role at own position", manager, role(roleFar, 5, 0), false},
			{"manager cannot manage higher role", manager, role(roleFar, 6, 0), false},
			{"admin manages lower role", admin, role(roleFar, 4, 0), true},
			{"admin cannot manage own-position role", admin, role(roleFar, 5, 0), false},
			{"no MANAGE_ROLES cannot manage", plain, role(roleFar, 1, 0), false},
			{"everyone role (position 0) is editable by manager", manager, everyone(0), true},
		}
		for _, tc := range tests {
			if got := permissions.CanManageRole(tc.actor, tc.target); got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	t.Run("CanGrant", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			actor permissions.Subject
			req   permissions.Permission
			want  bool
		}{
			{"owner grants Administrator", owner, permissions.Administrator, true},
			{"owner cannot grant unknown bits", owner, 1 << 50, false},
			{"admin cannot grant unknown bits", admin, permissions.All | 1<<50, false},
			{"admin grants anything", admin, permissions.All, true},
			{"subset is allowed", plain, permissions.ViewChannel, true},
			{"empty request is allowed", plain, 0, true},
			{"denies administrator the actor lacks", manager, permissions.Administrator, false},
			{"denies a mix with one missing bit", plain, permissions.ViewChannel | permissions.SendMessages, false},
			{"manager cannot grant what only others have", manager, permissions.KickMembers, false},
			{"manager can grant MANAGE_ROLES it has", manager, permissions.ManageRoles, true},
		}
		for _, tc := range tests {
			if got := permissions.CanGrant(tc.actor, tc.req); got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	t.Run("CanActOn", func(t *testing.T) {
		t.Parallel()
		ownerTarget := subject(0)
		ownerTarget.UserID = ownerID
		higher := subject(0, role(roleA, 9, 0))
		lower := subject(0, role(roleB, 2, 0))
		lower.UserID = otherID
		lowPlain := subject(0)
		lowPlain.UserID = otherID + 1
		tests := []struct {
			name          string
			actor, target permissions.Subject
			want          bool
		}{
			{"owner acts on anyone", owner, higher, true},
			{"nobody acts on owner", higher, ownerTarget, false},
			{"owner cannot act on owner either", owner, ownerTarget, false},
			{"higher acts on lower", higher, lower, true},
			{"lower cannot act on higher", lower, higher, false},
			{"equal positions are denied", subject(0, role(roleA, 4, 0)), func() permissions.Subject { s := subject(0, role(roleB, 4, 0)); s.UserID = otherID; return s }(), false},
			{"role-holder acts on role-less", lower, lowPlain, true},
		}
		for _, tc := range tests {
			if got := permissions.CanActOn(tc.actor, tc.target); got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

func TestPermissionJSON(t *testing.T) {
	t.Parallel()
	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		b, err := json.Marshal(permissions.ManageRoles)
		if err != nil || string(b) != `"268435456"` {
			t.Fatalf("got %s, %v", b, err)
		}
		b, err = json.Marshal(struct {
			P permissions.Permission `json:"p"`
		}{permissions.Permission(0)})
		if err != nil || string(b) != `{"p":"0"}` {
			t.Fatalf("got %s, %v", b, err)
		}
	})

	t.Run("unmarshal valid", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			in   string
			want permissions.Permission
		}{
			{`"0"`, 0},
			{`"268435456"`, permissions.ManageRoles},
			{`"18446744073709551615"`, permissions.Permission(^uint64(0))},
			{`"1099511627776"`, 1 << 40},
		}
		for _, tc := range tests {
			var p permissions.Permission
			if err := json.Unmarshal([]byte(tc.in), &p); err != nil || p != tc.want {
				t.Errorf("%s: got %d, %v; want %d", tc.in, p, err, tc.want)
			}
		}
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		t.Parallel()
		bad := []string{
			`268435456`, `null`, `true`, `{}`, `[]`, `""`, `"-1"`, `"+1"`, `"01"`, `"00"`,
			`" 1"`, `"1 "`, `"0x10"`, `"1e3"`, `"1.5"`, `"abc"`, `"١٢"`,
			`"18446744073709551616"`,
			`"100000000000000000000"`,
			`"` + strings.Repeat("1", 21) + `"`,
			`"` + strings.Repeat("0", 25) + `"`,
		}
		for _, in := range bad {
			p := permissions.Permission(7)
			if err := json.Unmarshal([]byte(in), &p); err == nil {
				t.Errorf("%s: expected error, got %d", in, p)
			}
		}
	})

	t.Run("roundtrip in struct", func(t *testing.T) {
		t.Parallel()
		type body struct {
			Allow permissions.Permission `json:"allow"`
		}
		var b body
		if err := json.Unmarshal([]byte(`{"allow":"1024"}`), &b); err != nil || b.Allow != permissions.ViewChannel {
			t.Fatalf("got %+v, %v", b, err)
		}
		if err := json.Unmarshal([]byte(`{"allow":1024}`), &b); err == nil {
			t.Fatal("number must be rejected")
		}
	})
}

func TestPermissionBits(t *testing.T) {
	t.Parallel()
	bits := map[string]struct {
		p    permissions.Permission
		want uint64
	}{
		"CREATE_INVITE": {permissions.CreateInvite, 1 << 0}, "KICK_MEMBERS": {permissions.KickMembers, 1 << 1},
		"BAN_MEMBERS": {permissions.BanMembers, 1 << 2}, "ADMINISTRATOR": {permissions.Administrator, 1 << 3},
		"MANAGE_CHANNELS": {permissions.ManageChannels, 1 << 4}, "MANAGE_GUILD": {permissions.ManageGuild, 1 << 5},
		"STREAM": {permissions.Stream, 1 << 9}, "VIEW_CHANNEL": {permissions.ViewChannel, 1 << 10},
		"SEND_MESSAGES": {permissions.SendMessages, 1 << 11}, "MANAGE_MESSAGES": {permissions.ManageMessages, 1 << 13},
		"ATTACH_FILES": {permissions.AttachFiles, 1 << 15}, "READ_MESSAGE_HISTORY": {permissions.ReadMessageHistory, 1 << 16},
		"MENTION_EVERYONE": {permissions.MentionEveryone, 1 << 17}, "CONNECT": {permissions.Connect, 1 << 20},
		"SPEAK": {permissions.Speak, 1 << 21}, "MUTE_MEMBERS": {permissions.MuteMembers, 1 << 22},
		"DEAFEN_MEMBERS": {permissions.DeafenMembers, 1 << 23}, "MOVE_MEMBERS": {permissions.MoveMembers, 1 << 24},
		"USE_VOICE_ACTIVITY": {permissions.UseVoiceActivity, 1 << 25}, "CHANGE_NICKNAME": {permissions.ChangeNickname, 1 << 26},
		"MANAGE_NICKNAMES": {permissions.ManageNicknames, 1 << 27}, "MANAGE_ROLES": {permissions.ManageRoles, 1 << 28},
	}
	var or uint64
	for name, b := range bits {
		if uint64(b.p) != b.want {
			t.Errorf("%s = %d, want %d", name, b.p, b.want)
		}
		or |= b.want
	}
	if uint64(permissions.All) != or {
		t.Errorf("All = %#x, want OR of constants %#x", uint64(permissions.All), or)
	}
	wantEveryone := permissions.ViewChannel | permissions.SendMessages | permissions.ReadMessageHistory |
		permissions.AttachFiles | permissions.MentionEveryone | permissions.CreateInvite |
		permissions.ChangeNickname | permissions.Connect | permissions.Speak | permissions.Stream |
		permissions.UseVoiceActivity
	if permissions.DefaultEveryone != wantEveryone {
		t.Errorf("DefaultEveryone = %#x, want %#x", uint64(permissions.DefaultEveryone), uint64(wantEveryone))
	}
	if !permissions.All.Has(permissions.ViewChannel | permissions.Speak) {
		t.Error("Has with multiple bits should be true when all set")
	}
	if permissions.ViewChannel.Has(permissions.ViewChannel | permissions.Speak) {
		t.Error("Has requires every bit")
	}
	if !permissions.Permission(0).Has(0) {
		t.Error("Has(0) is always true")
	}
}
