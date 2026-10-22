package guilds

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// DB is a database handle the guild reads can run through: the pool or a
// transaction of another module (so it sees the same snapshot and locks).
type DB = store.DBTX

// LockGuild takes the per-guild write lock inside tx until it ends. Role,
// member-role, channel and overwrite changes of one guild all take it, so
// they never interleave. A missing guild is ErrNotMember.
func (s *Service) LockGuild(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error {
	return s.lockGuild(ctx, tx, guildID)
}

// SubjectIn is Subject read through db.
func (s *Service) SubjectIn(ctx context.Context, db DB, guildID, userID snowflake.ID) (permissions.Subject, error) {
	return s.subject(ctx, db, guildID, userID)
}

// Lookup returns the guild without checking membership (invite previews);
// a missing guild is ErrNotFound.
func (s *Service) Lookup(ctx context.Context, guildID snowflake.ID) (Guild, error) {
	g, err := s.q.GuildByID(ctx, s.pool, guildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Guild{}, ErrNotFound
	}
	if err != nil {
		return Guild{}, fmt.Errorf("guilds: load guild: %w", err)
	}
	return toGuild(g), nil
}

// Access is the permission view of a whole guild at one moment: owner,
// roles and the roles of every member. It answers for any member without
// further queries.
type Access struct {
	GuildID, OwnerID snowflake.ID
	Everyone         permissions.Role
	roles            map[snowflake.ID]permissions.Role // without @everyone
	members          map[snowflake.ID][]snowflake.ID
	memberIDs        []snowflake.ID // ascending
}

// AccessIn loads the Access of guildID through db in three queries; a
// missing guild is ErrNotFound.
func (s *Service) AccessIn(ctx context.Context, db DB, guildID snowflake.ID) (Access, error) {
	g, err := s.q.GuildByID(ctx, db, guildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, ErrNotFound
	}
	if err != nil {
		return Access{}, fmt.Errorf("guilds: load guild: %w", err)
	}
	roles, err := s.roles(ctx, db, guildID)
	if err != nil {
		return Access{}, err
	}
	rows, err := s.q.MembersWithRoles(ctx, db, guildID)
	if err != nil {
		return Access{}, fmt.Errorf("guilds: list member roles: %w", err)
	}
	a := Access{GuildID: guildID, OwnerID: g.OwnerID,
		roles:     make(map[snowflake.ID]permissions.Role, len(roles)),
		members:   make(map[snowflake.ID][]snowflake.ID, len(rows)),
		memberIDs: make([]snowflake.ID, len(rows))}
	for _, r := range roles {
		if r.ID == guildID {
			a.Everyone = r.perm()
		} else {
			a.roles[r.ID] = r.perm()
		}
	}
	for i, m := range rows {
		ids := make([]snowflake.ID, len(m.RoleIds))
		for j, id := range m.RoleIds {
			ids[j] = snowflake.ID(id)
		}
		a.members[m.UserID] = ids
		a.memberIDs[i] = m.UserID
	}
	return a, nil
}

// MemberIDs returns the members, ascending.
func (a Access) MemberIDs() []snowflake.ID {
	return append([]snowflake.ID(nil), a.memberIDs...)
}

// IsMember reports whether userID is a member.
func (a Access) IsMember(userID snowflake.ID) bool {
	_, ok := a.members[userID]
	return ok
}

// Role returns a role of the guild, @everyone included.
func (a Access) Role(id snowflake.ID) (permissions.Role, bool) {
	if id == a.GuildID {
		return a.Everyone, true
	}
	r, ok := a.roles[id]
	return r, ok
}

// Subject returns userID as a permission subject; false for a non-member.
func (a Access) Subject(userID snowflake.ID) (permissions.Subject, bool) {
	ids, ok := a.members[userID]
	if !ok {
		return permissions.Subject{}, false
	}
	sub := permissions.Subject{UserID: userID, OwnerID: a.OwnerID, GuildID: a.GuildID,
		Everyone: a.Everyone, Roles: make([]permissions.Role, 0, len(ids))}
	for _, id := range ids {
		if r, found := a.roles[id]; found {
			sub.Roles = append(sub.Roles, r)
		}
	}
	return sub, true
}
