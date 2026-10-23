package permissions_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestInvalidSubjectFailsClosed(t *testing.T) {
	t.Parallel()
	var zero permissions.Subject
	member := subject(permissions.ViewChannel)

	if got := permissions.Base(zero); got != 0 {
		t.Errorf("Base(zero) = %#x", uint64(got))
	}
	if got := permissions.InChannel(zero, []permissions.Overwrite{roleOW(0, permissions.All, 0)}); got != 0 {
		t.Errorf("InChannel(zero) = %#x", uint64(got))
	}
	if got := permissions.HighestPosition(zero); got != -1 {
		t.Errorf("HighestPosition(zero) = %d", got)
	}
	if permissions.CanActOn(zero, member) {
		t.Error("CanActOn(zero, member) must be false")
	}
	if permissions.CanManageRole(zero, role(roleA, 1, 0)) {
		t.Error("CanManageRole(zero) must be false")
	}
	if permissions.CanGrant(zero, permissions.ViewChannel) {
		t.Error("CanGrant(zero) must be false")
	}

	noOwner := subject(permissions.ViewChannel)
	noOwner.OwnerID = 0
	noOwner.UserID = 0
	if permissions.Base(noOwner) != 0 {
		t.Error("subject with zero user and owner must not be owner")
	}
	noOwnerID := subject(permissions.ViewChannel)
	noOwnerID.OwnerID = 0
	if got := permissions.Base(noOwnerID); got != 0 {
		t.Errorf("member with OwnerID 0 got %#x, want 0", uint64(got))
	}
	noGuild := subject(permissions.ViewChannel)
	noGuild.GuildID = 0
	if permissions.Base(noGuild) != 0 {
		t.Error("subject without guild must get 0")
	}
}

func TestCanActOnCrossGuildAndSelf(t *testing.T) {
	t.Parallel()
	owner := subject(0)
	owner.UserID = ownerID
	other := subject(0)
	other.UserID = otherID
	other.GuildID = guildID + 1
	if permissions.CanActOn(owner, other) {
		t.Error("cross-guild CanActOn must be false")
	}
	hi := subject(0, role(roleA, 9, 0))
	if permissions.CanActOn(hi, hi) {
		t.Error("acting on self must be false")
	}
}

func TestInChannelMasksAdministratorInOverwrites(t *testing.T) {
	t.Parallel()
	const view = permissions.ViewChannel
	s := subject(view, role(roleA, 1, 0))
	for name, ow := range map[string]permissions.Overwrite{
		"everyone": roleOW(guildID, permissions.Administrator, 0),
		"role":     roleOW(roleA, permissions.Administrator, 0),
		"member":   memberOW(userID, permissions.Administrator, 0),
	} {
		got := permissions.InChannel(s, []permissions.Overwrite{ow})
		if got.Has(permissions.Administrator) || got != view {
			t.Errorf("%s: got %#x, want view only", name, uint64(got))
		}
	}
	// A deny of Administrator on a real admin does not matter either.
	adm := subject(0, role(roleA, 1, permissions.Administrator))
	if got := permissions.InChannel(adm, []permissions.Overwrite{roleOW(guildID, 0, permissions.Administrator)}); got != permissions.All {
		t.Errorf("admin = %#x", uint64(got))
	}
}

func TestCanGrantChangedBitsOnly(t *testing.T) {
	t.Parallel()
	manager := subject(0, role(roleA, 5, permissions.ManageRoles))
	// Role already has MANAGE_GUILD; the edit changes only VIEW_CHANNEL... which
	// the manager lacks too, so use a bit the manager has.
	if !permissions.CanGrant(manager, permissions.ManageRoles) {
		t.Error("changed bit the actor has must be grantable")
	}
	if permissions.CanGrant(manager, permissions.ManageRoles|permissions.ManageGuild) {
		t.Error("changed bit the actor lacks must be refused")
	}
	if !permissions.CanGrant(manager, 0) {
		t.Error("no changed bits must be allowed")
	}
}

func TestErrInvalid(t *testing.T) {
	t.Parallel()
	var p permissions.Permission
	if err := json.Unmarshal([]byte(`123`), &p); !errors.Is(err, permissions.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
