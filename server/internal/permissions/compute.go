package permissions

import "github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"

// Role is a server role. Position grows with rank; @everyone has 0.
type Role struct {
	ID          snowflake.ID
	Position    int
	Permissions Permission
}

// OverwriteType says what an Overwrite targets.
type OverwriteType int

const (
	// OverwriteRole targets a role (the @everyone role has the guild ID).
	OverwriteRole OverwriteType = 0
	// OverwriteMember targets a single member.
	OverwriteMember OverwriteType = 1
)

// Overwrite is a per-channel allow/deny pair for a role or a member.
type Overwrite struct {
	ID          snowflake.ID
	Type        OverwriteType
	Allow, Deny Permission
}

// Subject is a member as seen by the permission model.
type Subject struct {
	UserID, OwnerID, GuildID snowflake.ID
	Everyone                 Role
	// Roles are the member's roles without @everyone.
	Roles []Role
}

// valid reports whether all identifiers are set; a zero-valued Subject must
// never be mistaken for the owner, so invalid subjects get no rights at all.
func (s Subject) valid() bool { return s.UserID != 0 && s.GuildID != 0 && s.OwnerID != 0 }

func (s Subject) isOwner() bool { return s.valid() && s.UserID == s.OwnerID }

// Base returns the server-wide permissions of s.
func Base(s Subject) Permission {
	if !s.valid() {
		return 0
	}
	if s.isOwner() {
		return All
	}
	p := s.Everyone.Permissions
	for _, r := range s.Roles {
		p |= r.Permissions
	}
	if p.Has(Administrator) {
		return All
	}
	return p
}

// InChannel returns the permissions of s in a channel with the given
// overwrites. Order: base, administrator shortcut, @everyone overwrite,
// merged role overwrites, member overwrite; in each step deny is applied
// before allow. ADMINISTRATOR in an overwrite is ignored. Without
// VIEW_CHANNEL the result is 0. Guild-scoped actions must use Base, not this.
//
// If several overwrites target the same role or member (a programming error
// upstream), only the first one counts.
func InChannel(s Subject, overwrites []Overwrite) Permission {
	if !s.valid() {
		return 0
	}
	p := Base(s)
	if p.Has(Administrator) {
		return All
	}

	var (
		everyoneOW, memberOW *Overwrite
		roleAllow, roleDeny  Permission
		seenRoles            = make(map[snowflake.ID]struct{}, len(s.Roles))
	)
	for i := range overwrites {
		ow := &overwrites[i]
		switch {
		case ow.Type == OverwriteMember && ow.ID == s.UserID:
			if memberOW == nil {
				memberOW = ow
			}
		case ow.Type == OverwriteRole && ow.ID == s.GuildID:
			if everyoneOW == nil {
				everyoneOW = ow
			}
		case ow.Type == OverwriteRole && hasRole(s.Roles, ow.ID):
			if _, dup := seenRoles[ow.ID]; !dup {
				seenRoles[ow.ID] = struct{}{}
				roleAllow |= ow.Allow
				roleDeny |= ow.Deny
			}
		}
	}

	if everyoneOW != nil {
		p = p&^(everyoneOW.Deny&^Administrator) | (everyoneOW.Allow &^ Administrator)
	}
	p = p&^(roleDeny&^Administrator) | (roleAllow &^ Administrator)
	if memberOW != nil {
		p = p&^(memberOW.Deny&^Administrator) | (memberOW.Allow &^ Administrator)
	}

	if !p.Has(ViewChannel) {
		return 0
	}
	return p
}

func hasRole(roles []Role, id snowflake.ID) bool {
	for _, r := range roles {
		if r.ID == id {
			return true
		}
	}
	return false
}
