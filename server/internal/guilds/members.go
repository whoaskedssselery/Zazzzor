package guilds

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// MemberFields are the settable fields of a member; a nil field is left as
// it is.
type MemberFields struct {
	// Nick: a pointer to nil or to "" clears the nickname.
	Nick **string
	// Roles replaces the member's roles (without @everyone).
	Roles *[]snowflake.ID
}

// normalizeNick validates a nickname; nil or "" clear it.
func normalizeNick(nick *string) (*string, error) {
	if nick == nil || *nick == "" {
		return nil, nil
	}
	n, err := normalizeName("nick", *nick, 1, 32)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// UpdateMember changes the nickname and/or roles of targetID. Own nickname
// needs CHANGE_NICKNAME; another member's MANAGE_NICKNAMES and ranking above
// them (CanActOn). Roles need MANAGE_ROLES, and every role added or removed
// must be one the actor can manage (CanManageRole); the owner may change
// their own roles.
func (s *Service) UpdateMember(ctx context.Context, guildID, targetID, actorID snowflake.ID, f MemberFields) (Member, error) {
	var nick *string
	if f.Nick != nil {
		var err error
		if nick, err = normalizeNick(*f.Nick); err != nil {
			return Member{}, err
		}
	}
	var want []snowflake.ID
	if f.Roles != nil {
		if len(*f.Roles) > MaxRoles {
			return Member{}, invalid("roles", fmt.Sprintf("must list at most %d roles", MaxRoles))
		}
		want = slices.Clone(*f.Roles)
		slices.Sort(want)
		want = slices.Compact(want)
		if slices.Contains(want, guildID) {
			return Member{}, invalid("roles", "must not contain the @everyone role")
		}
	}

	var (
		updated      Member
		rolesChanged bool
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		rolesChanged = false
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		actor, err := s.subject(ctx, tx, guildID, actorID)
		if err != nil {
			return err
		}
		target, err := s.member(ctx, tx, guildID, targetID)
		if err != nil {
			return err
		}
		if f.Nick != nil {
			if err = s.checkNick(ctx, tx, actor, targetID); err != nil {
				return err
			}
			if _, err = s.q.SetNick(ctx, tx, store.SetNickParams{GuildID: guildID, UserID: targetID, Nick: nick}); err != nil {
				return fmt.Errorf("guilds: set nick: %w", err)
			}
			if deref(target.Nick) != deref(nick) {
				if err = s.record(ctx, tx, audit.Entry{GuildID: guildID, ActorID: actorID, Action: audit.MemberUpdate,
					TargetID: targetID, Changes: []audit.Change{{Key: "nick", Old: target.Nick, New: nick}}}); err != nil {
					return err
				}
			}
		}
		if f.Roles != nil {
			if rolesChanged, err = s.replaceRoles(ctx, tx, actor, guildID, targetID, target.Roles, want); err != nil {
				return err
			}
		}
		if rolesChanged {
			if err = s.record(ctx, tx, audit.Entry{GuildID: guildID, ActorID: actorID, Action: audit.MemberRoleUpdate,
				TargetID: targetID, Changes: roleDiff(target.Roles, want)}); err != nil {
				return err
			}
			if err = s.pruneReadStates(ctx, tx, guildID); err != nil {
				return err
			}
		}
		updated, err = s.member(ctx, tx, guildID, targetID)
		return err
	})
	if err != nil {
		return Member{}, err
	}
	s.publish(ctx, events.Event{Type: events.GuildMemberUpdate,
		Audience: events.Audience{Kind: events.AudienceGuild, GuildID: guildID},
		Payload:  MemberPayload{GuildID: guildID, Member: updated.Public()}})
	if rolesChanged {
		s.publishSync(ctx, guildID)
	}
	return updated, nil
}

func (s *Service) checkNick(ctx context.Context, tx pgx.Tx, actor permissions.Subject, targetID snowflake.ID) error {
	base := permissions.Base(actor)
	if targetID == actor.UserID {
		if !base.Has(permissions.ChangeNickname) {
			return ErrMissingPerm
		}
		return nil
	}
	if !base.Has(permissions.ManageNicknames) {
		return ErrMissingPerm
	}
	target, err := s.subject(ctx, tx, actor.GuildID, targetID)
	if err != nil {
		return err
	}
	if !permissions.CanActOn(actor, target) {
		return ErrMissingPerm
	}
	return nil
}

// replaceRoles sets the roles of targetID to want (sorted, unique, without
// @everyone) and reports whether anything changed.
func (s *Service) replaceRoles(ctx context.Context, tx pgx.Tx, actor permissions.Subject,
	guildID, targetID snowflake.ID, have, want []snowflake.ID,
) (bool, error) {
	if !permissions.Base(actor).Has(permissions.ManageRoles) {
		return false, ErrMissingPerm
	}
	var add, remove []snowflake.ID
	for _, id := range want {
		if !slices.Contains(have, id) {
			add = append(add, id)
		}
	}
	for _, id := range have {
		if !slices.Contains(want, id) {
			remove = append(remove, id)
		}
	}
	for _, id := range append(slices.Clone(add), remove...) {
		r, err := s.roleOf(ctx, tx, guildID, id)
		if errors.Is(err, ErrNotFound) {
			return false, invalid("roles", "lists a role that is not in this server")
		}
		if err != nil {
			return false, err
		}
		if !permissions.CanManageRole(actor, r.perm()) {
			return false, ErrMissingPerm
		}
	}
	if len(add) > 0 {
		if err := s.q.AddMemberRoles(ctx, tx, store.AddMemberRolesParams{GuildID: guildID, UserID: targetID, RoleIds: ids64(add)}); err != nil {
			return false, fmt.Errorf("guilds: add member roles: %w", err)
		}
	}
	if len(remove) > 0 {
		if err := s.q.RemoveMemberRoles(ctx, tx, store.RemoveMemberRolesParams{GuildID: guildID, UserID: targetID, RoleIds: ids64(remove)}); err != nil {
			return false, fmt.Errorf("guilds: remove member roles: %w", err)
		}
	}
	return len(add)+len(remove) > 0, nil
}
