package channels

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// CreateFields describe a new channel.
type CreateFields struct {
	Name     string
	Type     Type
	ParentID *snowflake.ID
	Topic    *string
}

// UpdateFields are the settable fields of a channel; nil leaves a field as
// it is.
type UpdateFields struct {
	Name *string
	// Topic: a pointer to nil (or to a blank string) clears the topic.
	Topic **string
	// ParentID: a pointer to nil moves the channel to the top level.
	ParentID **snowflake.ID
	// SyncPermissions true copies the category's overwrites and syncs the
	// channel with them; false keeps the current ones as the channel's own.
	SyncPermissions *bool
}

// Move asks for channel ID to be at Position within its parent, or within
// *ParentID when ParentID is set (a pointer to nil: the top level).
type Move struct {
	ID       snowflake.ID
	Position int
	ParentID **snowflake.ID
}

// locked runs fn in a transaction holding the guild lock, with the actor's
// subject and the guild's channels loaded. A non-member is
// guilds.ErrNotMember.
func (s *Service) locked(ctx context.Context, guildID, actorID snowflake.ID,
	fn func(tx pgx.Tx, actor permissions.Subject, l *layout) error,
) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.guilds.LockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		actor, err := s.guilds.SubjectIn(ctx, tx, guildID, actorID)
		if err != nil {
			return err
		}
		l, err := loadLayout(ctx, s.q, tx, guildID)
		if err != nil {
			return err
		}
		return fn(tx, actor, l)
	})
}

// guildOf returns the guild of channelID; a missing channel is ErrNotFound.
func (s *Service) guildOf(ctx context.Context, channelID snowflake.ID) (snowflake.ID, error) {
	row, err := s.q.ChannelByID(ctx, s.pool, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("channels: load channel: %w", err)
	}
	if row.GuildID == nil {
		return 0, ErrNotFound
	}
	return *row.GuildID, nil
}

// lockedChannel is locked for the guild of channelID, also handing fn the
// channel's node and the actor's permissions in it. A missing channel, a
// non-member and a channel the actor may not view are ErrNotFound.
func (s *Service) lockedChannel(ctx context.Context, channelID, actorID snowflake.ID,
	fn func(tx pgx.Tx, actor permissions.Subject, l *layout, n *node, perm permissions.Permission) error,
) error {
	guildID, err := s.guildOf(ctx, channelID)
	if err != nil {
		return err
	}
	err = s.locked(ctx, guildID, actorID, func(tx pgx.Tx, actor permissions.Subject, l *layout) error {
		n := l.nodes[channelID]
		if n == nil { // deleted before the lock was taken
			return ErrNotFound
		}
		perm := permissions.InChannel(actor, l.effective(n))
		if !perm.Has(permissions.ViewChannel) {
			return ErrNotFound
		}
		return fn(tx, actor, l, n, perm)
	})
	if errors.Is(err, guilds.ErrNotMember) {
		return ErrNotFound
	}
	return err
}

// category returns the category id of l that sub may view, else a
// validation error on field.
func (l *layout) category(sub permissions.Subject, id snowflake.ID, field string) (*node, error) {
	p := l.nodes[id]
	if p == nil || p.ch.Type != TypeCategory || !l.canView(sub, p) {
		return nil, invalid(field, "must be a category of this server")
	}
	return p, nil
}

// CreateChannel creates a channel at the end of its parent (or of the top
// level). The actor needs MANAGE_CHANNELS in the guild; a channel in a
// category starts synced with it.
func (s *Service) CreateChannel(ctx context.Context, guildID, actorID snowflake.ID, f CreateFields) (Channel, error) {
	if !f.Type.valid() {
		return Channel{}, invalid("type", "must be 0 (text), 2 (voice) or 4 (category)")
	}
	name, nameErr := normalizeName(f.Name)
	if nameErr != nil {
		return Channel{}, nameErr
	}
	topic, topicErr := normalizeTopic(f.Topic)
	if topicErr != nil {
		return Channel{}, topicErr
	}
	if topic != nil && f.Type != TypeText {
		return Channel{}, invalid("topic", "only text channels have a topic")
	}
	if f.ParentID != nil && f.Type == TypeCategory {
		return Channel{}, invalid("parent_id", "a category cannot be in a category")
	}
	var created Channel
	err := s.locked(ctx, guildID, actorID, func(tx pgx.Tx, actor permissions.Subject, l *layout) error {
		if !permissions.Base(actor).Has(permissions.ManageChannels) {
			return ErrMissingPerm
		}
		if len(l.nodes) >= MaxChannels {
			return ErrTooManyChannels
		}
		var parent snowflake.ID
		if f.ParentID != nil {
			if _, err := l.category(actor, *f.ParentID, "parent_id"); err != nil {
				return err
			}
			parent = *f.ParentID
		}
		row, err := s.q.CreateChannel(ctx, tx, store.CreateChannelParams{
			ID: s.ids.Next(), GuildID: int64(guildID), Type: int16(f.Type), Name: name, Topic: topic,
			ParentID: f.ParentID, Position: int32(len(l.scopes()[parent])), //nolint:gosec // G115: at most MaxChannels
			PermissionSynced: f.ParentID != nil, CreatedAt: s.now(),
		})
		if err != nil {
			return fmt.Errorf("channels: create channel: %w", err)
		}
		n := &node{ch: toChannel(row)}
		n.ch.Overwrites = []permissions.Overwrite{}
		l.nodes[n.ch.ID] = n
		created = l.view(n)
		changes := []audit.Change{{Key: "name", New: created.Name}, {Key: "type", New: created.Type}}
		if created.ParentID != nil {
			changes = append(changes, audit.Change{Key: "parent_id", New: created.ParentID.String()})
		}
		return audit.Record(ctx, s.audit, tx, audit.Entry{GuildID: guildID, ActorID: actorID,
			Action: audit.ChannelCreate, TargetID: created.ID, Changes: changes})
	})
	if err != nil {
		return Channel{}, err
	}
	s.publishChannel(ctx, events.ChannelCreate, created)
	return created, nil
}

// UpdateChannel changes the fields that are set. The actor needs
// MANAGE_CHANNELS in the channel, and MANAGE_ROLES there to change its
// sync; re-syncing is authorized like the overwrite changes it makes
// (authorizeOverwrites). A channel moved to another category goes last in
// it and, if it was synced, keeps its overwrites as its own (see place).
func (s *Service) UpdateChannel(ctx context.Context, channelID, actorID snowflake.ID, f UpdateFields) (Channel, error) {
	var (
		name  *string
		topic *string
	)
	if f.Name != nil {
		n, err := normalizeName(*f.Name)
		if err != nil {
			return Channel{}, err
		}
		name = &n
	}
	if f.Topic != nil {
		t, err := normalizeTopic(*f.Topic)
		if err != nil {
			return Channel{}, err
		}
		topic = t
	}

	var (
		result    Channel
		changed   []Channel
		infoDirty bool
		resync    bool
	)
	err := s.lockedChannel(ctx, channelID, actorID, func(tx pgx.Tx, actor permissions.Subject, l *layout, n *node, perm permissions.Permission) error {
		changed, infoDirty, resync = nil, false, false
		if !perm.Has(permissions.ManageChannels) {
			return ErrMissingPerm
		}
		before := n.ch
		if f.SyncPermissions != nil && !perm.Has(permissions.ManageRoles) {
			return ErrMissingPerm
		}
		if name != nil && *name != n.ch.Name {
			n.ch.Name, infoDirty = *name, true
		}
		if f.Topic != nil {
			if topic != nil && n.ch.Type != TypeText {
				return invalid("topic", "only text channels have a topic")
			}
			if !equalPtr(topic, n.ch.Topic) {
				n.ch.Topic, infoDirty = topic, true
			}
		}
		if f.ParentID != nil {
			if err := l.reparent(actor, n, *f.ParentID); err != nil {
				return err
			}
		}
		if f.SyncPermissions != nil {
			if *f.SyncPermissions && n.ch.ParentID != nil && !n.ch.PermissionSynced {
				// Syncing replaces the effective overwrites with the
				// category's: authorize it like an overwrite change.
				a, err := s.guilds.AccessIn(ctx, tx, l.guildID)
				if err != nil {
					return err
				}
				parent := l.nodes[*n.ch.ParentID]
				if err = authorizeOverwrites(a, actor, perm, l.effective(n), parent.ch.Overwrites); err != nil {
					return err
				}
				if err := l.setSync(n, true); err != nil {
					return err
				}
				if err := l.pruneReadStates(ctx, s.q, tx, a); err != nil {
					return err
				}
			} else if err := l.setSync(n, *f.SyncPermissions); err != nil {
				return err
			}
		}
		if infoDirty {
			if err := s.q.SetChannelInfo(ctx, tx, store.SetChannelInfoParams{ID: n.ch.ID, Name: n.ch.Name, Topic: n.ch.Topic}); err != nil {
				return fmt.Errorf("channels: update channel: %w", err)
			}
		}
		resync = n.parent() != n.wasParent || n.ch.PermissionSynced != n.wasSynced || n.owDirty
		var err error
		if changed, err = s.save(ctx, tx, l); err != nil {
			return err
		}
		result = l.view(n)
		if infoDirty && !slices.ContainsFunc(changed, func(c Channel) bool { return c.ID == n.ch.ID }) {
			changed = append(changed, result)
		}
		if diff := channelChanges(before, n.ch); len(diff) > 0 {
			return audit.Record(ctx, s.audit, tx, audit.Entry{GuildID: l.guildID, ActorID: actorID,
				Action: audit.ChannelUpdate, TargetID: n.ch.ID, Changes: diff})
		}
		return nil
	})
	if err != nil {
		return Channel{}, err
	}
	for _, c := range changed {
		s.publishChannel(ctx, events.ChannelUpdate, c)
	}
	if resync {
		s.publishSync(ctx, result.GuildID)
	}
	return result, nil
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// reparent moves n to the end of parent (nil: the top level).
func (l *layout) reparent(actor permissions.Subject, n *node, parent *snowflake.ID) error {
	var to snowflake.ID
	if parent != nil {
		if n.ch.Type == TypeCategory {
			return invalid("parent_id", "a category cannot be in a category")
		}
		if _, err := l.category(actor, *parent, "parent_id"); err != nil {
			return err
		}
		to = *parent
	}
	if to == n.parent() {
		return nil
	}
	scopes := l.scopes()
	from := n.parent()
	scopes[from] = slices.DeleteFunc(scopes[from], func(m *node) bool { return m == n })
	scopes[to] = append(scopes[to], n)
	l.place(scopes)
	return nil
}

// setSync syncs n with its category (copying the category's overwrites as
// its own) or, with sync false, keeps the current ones as its own.
func (l *layout) setSync(n *node, sync bool) error {
	if n.ch.ParentID == nil {
		if sync {
			return invalid("sync_permissions", "the channel is not in a category")
		}
		return nil
	}
	if sync == n.ch.PermissionSynced {
		return nil
	}
	// Either way the channel ends up with the category's overwrites: they
	// are the effective ones of a synced channel.
	n.ch.Overwrites = cloneOverwrites(l.nodes[*n.ch.ParentID].ch.Overwrites)
	n.ch.PermissionSynced = sync
	n.owDirty = true
	return nil
}

// ReorderChannels applies moves: each listed channel goes to its position
// within its (new) parent, the others keep their order around them, and
// positions end up dense (0..n-1) within every parent. The actor needs
// MANAGE_CHANNELS in the guild and must see every channel and category
// named. A synced channel that changes parent stops being synced (see
// place). It returns the channels the actor can see.
func (s *Service) ReorderChannels(ctx context.Context, guildID, actorID snowflake.ID, moves []Move) ([]Channel, error) {
	if len(moves) > MaxChannels {
		return nil, invalid("channels", fmt.Sprintf("must list at most %d channels", MaxChannels))
	}
	seen := make(map[snowflake.ID]bool, len(moves))
	for _, m := range moves {
		switch {
		case seen[m.ID]:
			return nil, invalid("channels", "lists a channel twice")
		case m.Position < 0:
			return nil, invalid("channels", "positions start at 0")
		}
		seen[m.ID] = true
	}

	var (
		result  []Channel
		changed []Channel
		resync  bool
	)
	err := s.locked(ctx, guildID, actorID, func(tx pgx.Tx, actor permissions.Subject, l *layout) error {
		resync = false
		if !permissions.Base(actor).Has(permissions.ManageChannels) {
			return ErrMissingPerm
		}
		scopes, err := l.reorder(actor, moves)
		if err != nil {
			return err
		}
		l.place(scopes)
		for _, n := range l.nodes {
			if n.parent() != n.wasParent || n.ch.PermissionSynced != n.wasSynced {
				resync = true
			}
		}
		if changed, err = s.save(ctx, tx, l); err != nil {
			return err
		}
		result = l.visibleTo(actor)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, c := range changed {
		s.publishChannel(ctx, events.ChannelUpdate, c)
	}
	if resync {
		s.publishSync(ctx, guildID)
	}
	return result, nil
}

// reorder returns the scopes of l with moves applied (not yet placed).
func (l *layout) reorder(actor permissions.Subject, moves []Move) (map[snowflake.ID][]*node, error) {
	type target struct {
		n        *node
		parent   snowflake.ID
		position int
	}
	targets := make([]target, 0, len(moves))
	taken := make(map[[2]int64]bool, len(moves))
	for _, m := range moves {
		n := l.nodes[m.ID]
		if n == nil || !l.canView(actor, n) {
			return nil, invalid("channels", "lists a channel that is not in this server")
		}
		parent := n.parent()
		if m.ParentID != nil {
			parent = 0
			if p := *m.ParentID; p != nil {
				if n.ch.Type == TypeCategory {
					return nil, invalid("channels", "a category cannot be in a category")
				}
				if _, err := l.category(actor, *p, "channels"); err != nil {
					return nil, err
				}
				parent = *p
			}
		}
		key := [2]int64{int64(parent), int64(m.Position)}
		if taken[key] {
			return nil, invalid("channels", "lists a position twice in one category")
		}
		taken[key] = true
		targets = append(targets, target{n: n, parent: parent, position: m.Position})
	}

	scopes := l.scopes()
	for _, t := range targets {
		from := t.n.parent()
		scopes[from] = slices.DeleteFunc(scopes[from], func(m *node) bool { return m == t.n })
	}
	slices.SortFunc(targets, func(a, b target) int { return a.position - b.position })
	for _, t := range targets {
		sc := scopes[t.parent]
		scopes[t.parent] = slices.Insert(sc, min(t.position, len(sc)), t.n)
	}
	return scopes, nil
}

// DeleteChannel deletes a channel; the actor needs MANAGE_CHANNELS in it.
// The channels of a deleted category move to the end of the top level,
// keeping its overwrites if they were synced with it. CHANNEL_DELETE goes
// to the members who could see the channel before.
func (s *Service) DeleteChannel(ctx context.Context, channelID, actorID snowflake.ID) error {
	var (
		deleted   Channel
		audience  []snowflake.ID
		changed   []Channel
		hadChilds bool
	)
	err := s.lockedChannel(ctx, channelID, actorID, func(tx pgx.Tx, _ permissions.Subject, l *layout, n *node, perm permissions.Permission) error {
		if !perm.Has(permissions.ManageChannels) {
			return ErrMissingPerm
		}
		a, err := s.guilds.AccessIn(ctx, tx, l.guildID)
		if err != nil {
			return err
		}
		deleted = l.view(n)
		audience = viewers(a, deleted.Overwrites, nil)

		scopes := l.scopes()
		from := n.parent()
		scopes[from] = slices.DeleteFunc(scopes[from], func(m *node) bool { return m == n })
		children := scopes[n.ch.ID]
		hadChilds = len(children) > 0
		delete(scopes, n.ch.ID)
		scopes[0] = append(scopes[0], children...)
		l.place(scopes) // the children copy the category's overwrites while it is still there
		delete(l.nodes, n.ch.ID)

		if changed, err = s.save(ctx, tx, l); err != nil {
			return err
		}
		if err = s.q.DeleteChannel(ctx, tx, n.ch.ID); err != nil {
			return fmt.Errorf("channels: delete channel: %w", err)
		}
		return audit.Record(ctx, s.audit, tx, audit.Entry{GuildID: l.guildID, ActorID: actorID,
			Action: audit.ChannelDelete, TargetID: n.ch.ID,
			Changes: []audit.Change{{Key: "name", Old: deleted.Name}, {Key: "type", Old: deleted.Type}}})
	})
	if err != nil {
		return err
	}
	s.publish(ctx, events.Event{Type: events.ChannelDelete,
		Audience: events.Audience{Kind: events.AudienceUsers, GuildID: deleted.GuildID, UserIDs: audience},
		Payload:  deleted.Public()})
	for _, c := range changed {
		s.publishChannel(ctx, events.ChannelUpdate, c)
	}
	if hadChilds {
		s.publishSync(ctx, deleted.GuildID)
	}
	return nil
}

// channelChanges lists what an update changed in a channel, for the audit
// log.
func channelChanges(before, after Channel) []audit.Change {
	var out []audit.Change
	if before.Name != after.Name {
		out = append(out, audit.Change{Key: "name", Old: before.Name, New: after.Name})
	}
	if !equalPtr(before.Topic, after.Topic) {
		out = append(out, audit.Change{Key: "topic", Old: before.Topic, New: after.Topic})
	}
	if !equalPtr(before.ParentID, after.ParentID) {
		out = append(out, audit.Change{Key: "parent_id", Old: idText(before.ParentID), New: idText(after.ParentID)})
	}
	if before.PermissionSynced != after.PermissionSynced {
		out = append(out, audit.Change{Key: "permission_synced", Old: before.PermissionSynced, New: after.PermissionSynced})
	}
	return out
}

// idText is an optional ID as a string, nil for none.
func idText(id *snowflake.ID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
