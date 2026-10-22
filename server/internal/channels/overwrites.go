package channels

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// OverwriteFields are the new allow/deny pair for a role or member.
type OverwriteFields struct {
	Type        permissions.OverwriteType
	Allow, Deny permissions.Permission
}

func (f OverwriteFields) validate() error {
	switch {
	case f.Type != permissions.OverwriteRole && f.Type != permissions.OverwriteMember:
		return invalid("type", "must be 0 (role) or 1 (member)")
	case f.Allow&^permissions.All != 0:
		return invalid("allow", "contains unknown permission bits")
	case f.Deny&^permissions.All != 0:
		return invalid("deny", "contains unknown permission bits")
	case f.Allow.Has(permissions.Administrator):
		return invalid("allow", "ADMINISTRATOR cannot be set in a channel")
	case f.Deny.Has(permissions.Administrator):
		return invalid("deny", "ADMINISTRATOR cannot be set in a channel")
	case f.Allow&f.Deny != 0:
		return invalid("deny", "must not share bits with allow")
	}
	return nil
}

// PutOverwrite sets the overwrite of targetID (a role of the guild or a
// member) in the channel and returns the channel. The actor needs
// MANAGE_ROLES in the channel, must hold every bit that changes (CanGrant)
// and, for a role, rank above it (the owner may change any). The channel
// stops being synced with its category: it keeps the category's overwrites
// as its own, with this change.
func (s *Service) PutOverwrite(ctx context.Context, channelID, targetID, actorID snowflake.ID, f OverwriteFields) (Channel, error) {
	if err := f.validate(); err != nil {
		return Channel{}, err
	}
	next := permissions.Overwrite{ID: targetID, Type: f.Type, Allow: f.Allow, Deny: f.Deny}
	return s.changeOverwrite(ctx, channelID, targetID, actorID, &next)
}

// DeleteOverwrite removes the overwrite of targetID from the channel under
// the rules of PutOverwrite; removing one that is not there changes
// nothing. An overwrite of a deleted role or of a former member can always
// be removed.
func (s *Service) DeleteOverwrite(ctx context.Context, channelID, targetID, actorID snowflake.ID) error {
	_, err := s.changeOverwrite(ctx, channelID, targetID, actorID, nil)
	return err
}

// changeOverwrite replaces (next set) or removes (next nil) the overwrite
// of targetID.
func (s *Service) changeOverwrite(ctx context.Context, channelID, targetID, actorID snowflake.ID,
	next *permissions.Overwrite,
) (Channel, error) {
	guildID, lookupErr := s.guildOf(ctx, channelID)
	if lookupErr != nil {
		return Channel{}, lookupErr
	}
	var (
		result  Channel
		changed bool
		// synced are the channels following a changed category.
		synced []Channel
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		changed, synced = false, nil
		if err := s.guilds.LockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		a, err := s.guilds.AccessIn(ctx, tx, guildID)
		if err != nil {
			return err
		}
		actor, ok := a.Subject(actorID)
		if !ok {
			return ErrNotFound
		}
		l, err := s.loadLayout(ctx, tx, guildID)
		if err != nil {
			return err
		}
		n := l.nodes[channelID]
		if n == nil {
			return ErrNotFound
		}
		eff := l.effective(n)
		perm := permissions.InChannel(actor, eff)
		if !perm.Has(permissions.ViewChannel) {
			return ErrNotFound
		}
		if !perm.Has(permissions.ManageRoles) {
			return ErrMissingPerm
		}
		if err = checkTarget(a, actor, targetID, next); err != nil {
			return err
		}

		i := slices.IndexFunc(eff, func(o permissions.Overwrite) bool { return o.ID == targetID })
		var old permissions.Overwrite
		if i >= 0 {
			old = eff[i]
		}
		var diff permissions.Permission
		if next != nil {
			diff = (old.Allow ^ next.Allow) | (old.Deny ^ next.Deny)
		} else {
			diff = old.Allow | old.Deny
		}
		if !permissions.CanGrant(actor, diff) {
			return ErrMissingPerm
		}

		set := cloneOverwrites(eff)
		switch {
		case next != nil && i >= 0 && *next == old:
			result = l.view(n)
			return nil // nothing changes
		case next != nil && i >= 0:
			set[i] = *next
		case next != nil:
			set = append(set, *next)
			slices.SortFunc(set, func(x, y permissions.Overwrite) int { return cmp.Compare(x.ID, y.ID) })
		case i >= 0:
			set = slices.Delete(set, i, i+1)
		default:
			result = l.view(n)
			return nil // nothing to remove
		}
		n.ch.Overwrites, n.ch.PermissionSynced, n.owDirty = set, false, true
		if _, err = s.save(ctx, tx, l); err != nil {
			return err
		}
		result, changed = l.view(n), true
		for _, m := range l.sorted() {
			if m.ch.PermissionSynced && m.parent() == n.ch.ID {
				synced = append(synced, l.view(m))
			}
		}
		return nil
	})
	if errors.Is(err, guilds.ErrNotMember) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	if changed {
		s.publishChannel(ctx, events.ChannelUpdate, result)
		for _, c := range synced {
			s.publishChannel(ctx, events.ChannelUpdate, c)
		}
		s.publishSync(ctx, guildID)
	}
	return result, nil
}

// checkTarget checks the target of a new overwrite (next set) is a role or
// member of the guild, and that the actor ranks above a target role.
func checkTarget(a guilds.Access, actor permissions.Subject, targetID snowflake.ID, next *permissions.Overwrite) error {
	if next != nil && next.Type == permissions.OverwriteMember {
		if !a.IsMember(targetID) {
			return ErrNotFound
		}
		return nil
	}
	role, ok := a.Role(targetID)
	switch {
	case ok:
		if permissions.HighestPosition(actor) <= role.Position {
			return ErrMissingPerm
		}
	case next != nil:
		return ErrNotFound // no such role
	}
	return nil // removing the overwrite of a member, a former member or a deleted role
}
