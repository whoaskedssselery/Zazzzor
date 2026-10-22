package guilds

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// maxColor is the largest RGB color.
const maxColor = 0xFFFFFF

// RoleFields are the settable fields of a role; nil leaves a field as it is
// (on creation: name required, permissions 0, color 0, flags false).
type RoleFields struct {
	Name        *string
	Permissions *permissions.Permission
	Color       *int
	Hoist       *bool
	Mentionable *bool
}

// RolePosition asks for role ID to be at Position.
type RolePosition struct {
	ID       snowflake.ID
	Position int
}

func validColor(c *int) error {
	if c != nil && (*c < 0 || *c > maxColor) {
		return invalid("color", "must be between 0 and 16777215")
	}
	return nil
}

func validMask(p *permissions.Permission) error {
	if p != nil && *p&^permissions.All != 0 {
		return invalid("permissions", "contains unknown permission bits")
	}
	return nil
}

// Roles returns the guild's roles by position; the caller must be a member.
func (s *Service) Roles(ctx context.Context, guildID, actorID snowflake.ID) ([]Role, error) {
	ok, err := s.IsMember(ctx, guildID, actorID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMember
	}
	return s.roles(ctx, s.pool, guildID)
}

// roleOf loads a role of guildID; a role of another guild is ErrNotFound.
func (s *Service) roleOf(ctx context.Context, dbtx store.DBTX, guildID, roleID snowflake.ID) (Role, error) {
	r, err := s.q.RoleByID(ctx, dbtx, store.RoleByIDParams{GuildID: guildID, ID: roleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	if err != nil {
		return Role{}, fmt.Errorf("guilds: load role: %w", err)
	}
	return toRole(r), nil
}

// CreateRole creates a role right above @everyone, moving the others up.
// The actor needs MANAGE_ROLES and must hold every permission it grants.
func (s *Service) CreateRole(ctx context.Context, guildID, actorID snowflake.ID, f RoleFields) (Role, error) {
	if f.Name == nil {
		return Role{}, invalid("name", "is required")
	}
	name, nameErr := normalizeName("name", *f.Name, 1, 100)
	if nameErr != nil {
		return Role{}, nameErr
	}
	if err := validColor(f.Color); err != nil {
		return Role{}, err
	}
	if err := validMask(f.Permissions); err != nil {
		return Role{}, err
	}
	p := deref(f.Permissions)
	var created Role
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		actor, err := s.require(ctx, tx, guildID, actorID, permissions.ManageRoles)
		if err != nil {
			return err
		}
		if !permissions.CanGrant(actor, p) {
			return ErrMissingPerm
		}
		n, err := s.q.CountRoles(ctx, tx, guildID)
		if err != nil {
			return fmt.Errorf("guilds: count roles: %w", err)
		}
		if n >= MaxRoles {
			return ErrTooManyRoles
		}
		if err = s.q.ShiftRolesUp(ctx, tx, guildID); err != nil {
			return fmt.Errorf("guilds: shift roles: %w", err)
		}
		row, err := s.q.CreateRole(ctx, tx, store.CreateRoleParams{
			ID: s.ids.Next(), GuildID: guildID, Name: name, Color: int32(deref(f.Color)), //nolint:gosec // G115: validated
			Position: 1, Permissions: mask(p), Hoist: deref(f.Hoist), Mentionable: deref(f.Mentionable),
			CreatedAt: s.now(),
		})
		if err != nil {
			return fmt.Errorf("guilds: create role: %w", err)
		}
		created = toRole(row)
		return nil
	})
	if err != nil {
		return Role{}, err
	}
	s.publishRole(ctx, events.GuildRoleCreate, created)
	s.publishSync(ctx, guildID)
	return created, nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func (s *Service) publishRole(ctx context.Context, typ string, r Role) {
	s.publish(ctx, events.Event{Type: typ,
		Audience: events.Audience{Kind: events.AudienceGuild, GuildID: r.GuildID},
		Payload:  RolePayload{GuildID: r.GuildID, Role: r.Public()}})
}

// UpdateRole changes the fields of a role that are set. The actor must rank
// above the role (CanManageRole) and hold every permission bit it changes.
// Of @everyone only the permissions can change.
func (s *Service) UpdateRole(ctx context.Context, guildID, roleID, actorID snowflake.ID, f RoleFields) (Role, error) {
	if err := validColor(f.Color); err != nil {
		return Role{}, err
	}
	if err := validMask(f.Permissions); err != nil {
		return Role{}, err
	}
	var name *string
	if f.Name != nil {
		n, err := normalizeName("name", *f.Name, 1, 100)
		if err != nil {
			return Role{}, err
		}
		name = &n
	}
	if roleID == guildID {
		switch {
		case f.Name != nil:
			return Role{}, invalid("name", "the @everyone role cannot be renamed")
		case f.Color != nil:
			return Role{}, invalid("color", "the @everyone role has no color")
		case f.Hoist != nil:
			return Role{}, invalid("hoist", "the @everyone role cannot be hoisted")
		case f.Mentionable != nil:
			return Role{}, invalid("mentionable", "the @everyone role is always mentionable by MENTION_EVERYONE")
		}
	}
	var (
		updated Role
		changed bool
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		actor, err := s.subject(ctx, tx, guildID, actorID)
		if err != nil {
			return err
		}
		old, err := s.roleOf(ctx, tx, guildID, roleID)
		if err != nil {
			return err
		}
		if !permissions.CanManageRole(actor, old.perm()) {
			return ErrMissingPerm
		}
		next := old
		if name != nil {
			next.Name = *name
		}
		if f.Color != nil {
			next.Color = *f.Color
		}
		if f.Hoist != nil {
			next.Hoist = *f.Hoist
		}
		if f.Mentionable != nil {
			next.Mentionable = *f.Mentionable
		}
		if f.Permissions != nil {
			next.Permissions = *f.Permissions
			if !permissions.CanGrant(actor, next.Permissions^old.Permissions) {
				return ErrMissingPerm
			}
		}
		if next == old {
			updated = old
			return nil
		}
		row, err := s.q.UpdateRole(ctx, tx, store.UpdateRoleParams{GuildID: guildID, ID: roleID, Name: next.Name,
			Color: int32(next.Color), Permissions: mask(next.Permissions), //nolint:gosec // G115: validated
			Hoist: next.Hoist, Mentionable: next.Mentionable})
		if err != nil {
			return fmt.Errorf("guilds: update role: %w", err)
		}
		updated, changed = toRole(row), true
		return nil
	})
	if err != nil {
		return Role{}, err
	}
	if changed {
		s.publishRole(ctx, events.GuildRoleUpdate, updated)
		s.publishSync(ctx, guildID)
	}
	return updated, nil
}

// ReorderRoles moves the listed roles to the positions asked for; the
// others keep their relative order and every role ends up at a dense
// position 1..n. @everyone cannot be moved. Every role whose position
// changes must be one the actor can manage, and must end up below the
// actor's highest role. It returns all roles by position.
func (s *Service) ReorderRoles(ctx context.Context, guildID, actorID snowflake.ID, moves []RolePosition) ([]Role, error) {
	if len(moves) > MaxRoles {
		return nil, invalid("roles", fmt.Sprintf("must list at most %d roles", MaxRoles))
	}
	seenID := make(map[snowflake.ID]bool, len(moves))
	seenPos := make(map[int]bool, len(moves))
	for _, m := range moves {
		switch {
		case m.ID == guildID:
			return nil, invalid("roles", "the @everyone role cannot be moved")
		case seenID[m.ID]:
			return nil, invalid("roles", "lists a role twice")
		case seenPos[m.Position]:
			return nil, invalid("roles", "lists a position twice")
		case m.Position < 1:
			return nil, invalid("roles", "positions start at 1")
		}
		seenID[m.ID], seenPos[m.Position] = true, true
	}

	var (
		result []Role
		moved  []Role
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		moved = nil
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		actor, err := s.require(ctx, tx, guildID, actorID, permissions.ManageRoles)
		if err != nil {
			return err
		}
		all, err := s.roles(ctx, tx, guildID)
		if err != nil {
			return err
		}
		order, err := reorder(all[1:], moves) // all[0] is @everyone
		if err != nil {
			return err
		}
		top := permissions.HighestPosition(actor)
		var ids []int64
		var positions []int32
		for i, r := range order {
			pos := i + 1
			if r.Position == pos {
				continue
			}
			if !permissions.CanManageRole(actor, r.perm()) || pos >= top {
				return ErrMissingPerm
			}
			ids = append(ids, int64(r.ID))
			positions = append(positions, int32(pos)) //nolint:gosec // G115: at most MaxRoles
			r.Position = pos
			order[i] = r
			moved = append(moved, r)
		}
		if len(ids) > 0 {
			if err = s.q.SetRolePositions(ctx, tx, store.SetRolePositionsParams{GuildID: guildID, Ids: ids, Positions: positions}); err != nil {
				return fmt.Errorf("guilds: reorder roles: %w", err)
			}
		}
		result = append([]Role{all[0]}, order...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, r := range moved {
		s.publishRole(ctx, events.GuildRoleUpdate, r)
	}
	if len(moved) > 0 {
		s.publishSync(ctx, guildID)
	}
	return result, nil
}

// reorder returns roles (by position, without @everyone) with each moved
// role at index Position-1 (or last, past the end) and the others in their
// order around them.
func reorder(roles []Role, moves []RolePosition) ([]Role, error) {
	byID := make(map[snowflake.ID]Role, len(roles))
	for _, r := range roles {
		byID[r.ID] = r
	}
	movedIDs := make(map[snowflake.ID]bool, len(moves))
	for _, m := range moves {
		if _, ok := byID[m.ID]; !ok {
			return nil, invalid("roles", "lists a role that is not in this server")
		}
		movedIDs[m.ID] = true
	}
	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		if !movedIDs[r.ID] {
			out = append(out, r)
		}
	}
	sorted := slices.Clone(moves)
	slices.SortFunc(sorted, func(a, b RolePosition) int { return a.Position - b.Position })
	for _, m := range sorted {
		i := min(m.Position-1, len(out))
		out = slices.Insert(out, i, byID[m.ID])
	}
	return out, nil
}

// DeleteRole deletes a role the actor can manage; @everyone cannot be
// deleted. The roles above it move down one position.
func (s *Service) DeleteRole(ctx context.Context, guildID, roleID, actorID snowflake.ID) error {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		actor, err := s.subject(ctx, tx, guildID, actorID)
		if err != nil {
			return err
		}
		r, err := s.roleOf(ctx, tx, guildID, roleID)
		if err != nil {
			return err
		}
		if r.ID == guildID {
			return ErrEveryone
		}
		if !permissions.CanManageRole(actor, r.perm()) {
			return ErrMissingPerm
		}
		if _, err = s.q.DeleteRole(ctx, tx, store.DeleteRoleParams{GuildID: guildID, ID: roleID}); err != nil {
			return fmt.Errorf("guilds: delete role: %w", err)
		}
		if err = s.q.CloseRoleGap(ctx, tx, store.CloseRoleGapParams{GuildID: guildID, Position: int32(r.Position)}); err != nil { //nolint:gosec // G115: positions are small
			return fmt.Errorf("guilds: close role gap: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, events.Event{Type: events.GuildRoleDelete,
		Audience: events.Audience{Kind: events.AudienceGuild, GuildID: guildID},
		Payload:  RoleDeletePayload{GuildID: guildID, RoleID: roleID}})
	s.publishSync(ctx, guildID)
	return nil
}
