package permissions

import "math"

// HighestPosition returns the rank of s: the owner outranks everything, other
// members have the position of their highest role (0 with only @everyone).
func HighestPosition(s Subject) int {
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

// CanManageRole reports whether actor may edit target: it needs MANAGE_ROLES
// (or administrator) and must rank strictly above the role. @everyone may be
// edited this way; forbidding its deletion is the caller's job.
func CanManageRole(actor Subject, target Role) bool {
	return Base(actor).Has(ManageRoles) && HighestPosition(actor) > target.Position
}

// CanGrant reports whether actor may hand out the requested permissions:
// owners and administrators any, everyone else only a subset of their own.
func CanGrant(actor Subject, requested Permission) bool {
	base := Base(actor)
	if base.Has(Administrator) {
		return true
	}
	return base.Has(requested)
}

// CanActOn reports whether actor may act on target (kick, ban, assign roles).
// Nobody, the owner included, may act on the owner.
func CanActOn(actor, target Subject) bool {
	if target.isOwner() {
		return false
	}
	if actor.isOwner() {
		return true
	}
	return HighestPosition(actor) > HighestPosition(target)
}
