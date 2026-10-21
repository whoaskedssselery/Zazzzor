package permissions

import "math"

// HighestPosition returns the rank of s: the owner outranks everything, other
// members have the position of their highest role (0 with only @everyone).
// An invalid subject ranks -1, below everyone.
func HighestPosition(s Subject) int {
	if !s.valid() {
		return -1
	}
	if s.isOwner() {
		return math.MaxInt
	}
	pos := s.Everyone.Position
	for _, r := range s.Roles {
		if r.Position > pos {
			pos = r.Position
		}
	}
	return pos
}

// CanManageRole reports whether actor may edit or assign target: it needs
// MANAGE_ROLES (or administrator) and must rank strictly above the role.
// Role assignment goes through this check only; the owner may assign roles
// to themselves. @everyone may be edited this way; forbidding its deletion is
// the caller's job.
func CanManageRole(actor Subject, target Role) bool {
	if !actor.valid() {
		return false
	}
	return Base(actor).Has(ManageRoles) && HighestPosition(actor) > target.Position
}

// CanGrant reports whether actor may change the given permission bits.
// Callers pass only the bits that change: newMask ^ oldMask for an edit, the
// full mask for a creation. Owners and administrators may change any defined
// bit, everyone else only bits they hold themselves. Bits outside All are
// always refused.
func CanGrant(actor Subject, changed Permission) bool {
	if !actor.valid() || changed&^All != 0 {
		return false
	}
	base := Base(actor)
	if base.Has(Administrator) {
		return true
	}
	return base.Has(changed)
}

// CanActOn reports whether actor may moderate target, another member
// (others' nicknames, later kick and ban). Acting on oneself, on the owner
// or across guilds is refused; role assignment uses CanManageRole instead.
func CanActOn(actor, target Subject) bool {
	if !actor.valid() || !target.valid() || actor.GuildID != target.GuildID ||
		actor.UserID == target.UserID || target.isOwner() {
		return false
	}
	if actor.isOwner() {
		return true
	}
	return HighestPosition(actor) > HighestPosition(target)
}
