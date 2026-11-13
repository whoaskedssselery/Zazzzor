package channels

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
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
		l, err := loadLayout(ctx, s.q, tx, guildID)
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
		if next != nil {
			if err = targetExists(a, *next); err != nil {
				return err
			}
		}

		i := slices.IndexFunc(eff, func(o permissions.Overwrite) bool { return o.ID == targetID })
		set := cloneOverwrites(eff)
		switch {
		case next != nil && i >= 0 && *next == eff[i]:
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
		if err = authorizeOverwrites(a, actor, perm, eff, set); err != nil {
			return err
		}
		if err = recordOverwrite(ctx, s.audit, tx, l.guildID, actorID, channelID, targetID, eff, i, next); err != nil {
			return err
		}
		n.ch.Overwrites, n.ch.PermissionSynced, n.owDirty = set, false, true
		if _, err = s.save(ctx, tx, l); err != nil {
			return err
		}
		if err := l.pruneReadStates(ctx, s.q, tx, a); err != nil {
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

// targetExists checks the target of a new overwrite is a role or member of
// the guild.
func targetExists(a guilds.Access, ow permissions.Overwrite) error {
	if ow.Type == permissions.OverwriteMember {
		if !a.IsMember(ow.ID) {
			return ErrNotFound
		}
		return nil
	}
	if _, ok := a.Role(ow.ID); !ok {
		return ErrNotFound
	}
	return nil
}

// authorizeOverwrites checks that actor, whose permissions in the channel
// are perm, may replace the effective overwrites from with to: it needs
// MANAGE_ROLES there, must hold every allow or deny bit that changes for
// any target (CanGrant) and must rank above every role whose overwrite
// changes (the owner ranks above all). Member targets have no rank check,
// as in Discord; a target role that no longer exists has none either.
func authorizeOverwrites(a guilds.Access, actor permissions.Subject, perm permissions.Permission,
	from, to []permissions.Overwrite,
) error {
	if !perm.Has(permissions.ManageRoles) {
		return ErrMissingPerm
	}
	type pair struct{ old, new permissions.Overwrite }
	byTarget := make(map[snowflake.ID]*pair, len(from)+len(to))
	get := func(id snowflake.ID) *pair {
		p := byTarget[id]
		if p == nil {
			p = &pair{}
			byTarget[id] = p
		}
		return p
	}
	for _, o := range from {
		get(o.ID).old = o
	}
	for _, o := range to {
		get(o.ID).new = o
	}
	top := permissions.HighestPosition(actor)
	for id, p := range byTarget {
		if p.old == p.new {
			continue
		}
		diff := (p.old.Allow ^ p.new.Allow) | (p.old.Deny ^ p.new.Deny)
		if !permissions.CanGrant(actor, diff) {
			return ErrMissingPerm
		}
		if role, ok := a.Role(id); ok && top <= role.Position {
			return ErrMissingPerm
		}
	}
	return nil
}

// recordOverwrite writes the audit entry of an overwrite change: old is the
// effective set, i the index of targetID's overwrite in it (-1: none), next
// the new one (nil: removed).
func recordOverwrite(ctx context.Context, rec audit.Recorder, tx pgx.Tx, guildID, actorID, channelID, targetID snowflake.ID,
	old []permissions.Overwrite, i int, next *permissions.Overwrite,
) error {
	mask := func(p permissions.Permission) string { return strconv.FormatUint(uint64(p), 10) }
	e := audit.Entry{GuildID: guildID, ActorID: actorID, TargetID: channelID,
		Options: map[string]string{"id": targetID.String()}}
	switch {
	case next == nil:
		e.Action = audit.OverwriteDelete
		e.Changes = []audit.Change{{Key: "allow", Old: mask(old[i].Allow)}, {Key: "deny", Old: mask(old[i].Deny)}}
		e.Options["type"] = strconv.Itoa(int(old[i].Type))
	case i < 0:
		e.Action = audit.OverwriteCreate
		e.Changes = []audit.Change{{Key: "allow", New: mask(next.Allow)}, {Key: "deny", New: mask(next.Deny)}}
		e.Options["type"] = strconv.Itoa(int(next.Type))
	default:
		e.Action = audit.OverwriteUpdate
		e.Changes = []audit.Change{{Key: "allow", Old: mask(old[i].Allow), New: mask(next.Allow)},
			{Key: "deny", Old: mask(old[i].Deny), New: mask(next.Deny)}}
		e.Options["type"] = strconv.Itoa(int(next.Type))
	}
	return audit.Record(ctx, rec, tx, e)
}
