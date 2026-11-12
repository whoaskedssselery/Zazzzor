package channels

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	maxGroupRecipients = 10
	maxGroupsPerUser   = 100
)

// CreateGroup creates an unnamed group with the caller and 2–9 friends.
func (s *Service) CreateGroup(ctx context.Context, actor snowflake.ID, others []snowflake.ID) (DMChannel, error) {
	if ok, retry := s.groupLimiter.Allow(actor.String()); !ok {
		return DMChannel{}, httpx.TooManyRequests(retry)
	}
	if len(others) < 2 || len(others) >= maxGroupRecipients {
		return DMChannel{}, invalid("recipient_ids", "supply two to nine other users")
	}
	users := []int64{int64(actor)}
	for _, id := range others {
		if id <= 0 || slices.Contains(users, int64(id)) {
			return DMChannel{}, invalid("recipient_ids", "must contain distinct other users")
		}
		users = append(users, int64(id))
	}
	id := s.ids.Next()
	defer s.LockPrivateEvents(id)()
	var snapshot privateSnapshot
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.groupCandidates(ctx, tx, actor, users, users[1:]); err != nil {
			return err
		}
		if err := s.checkGroupCapacity(ctx, tx, users); err != nil {
			return err
		}
		now := s.now()
		if _, err := s.q.CreatePrivateChannel(ctx, tx, store.CreatePrivateChannelParams{
			ID: id, Type: int16(TypeGroupDM), OwnerID: &actor, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("channels: insert group DM: %w", err)
		}
		if err := s.q.AddPrivateRecipients(ctx, tx, store.AddPrivateRecipientsParams{
			ChannelID: int64(id), UserIds: users, JoinedAt: now,
		}); err != nil {
			return fmt.Errorf("channels: insert group recipients: %w", err)
		}
		var err error
		snapshot, err = s.privateSnapshot(ctx, tx, id)
		return err
	})
	if err != nil {
		return DMChannel{}, fmt.Errorf("channels: create group: %w", err)
	}
	for _, event := range snapshot.groupEvents(events.DMChannelCreate) {
		s.publish(ctx, event)
	}
	return snapshot.view(actor), nil
}

// groupCandidates locks the affected accounts before checking friendship and quotas.
func (s *Service) groupCandidates(ctx context.Context, tx pgx.Tx, actor snowflake.ID, locked, invited []int64) error {
	people, err := s.q.LockPrivateAccounts(ctx, tx, locked)
	if err != nil {
		return fmt.Errorf("channels: lock group recipients: %w", err)
	}
	if len(people) != len(locked) {
		return notFriend()
	}
	friends, err := s.q.GroupFriendIDs(ctx, tx, store.GroupFriendIDsParams{ActorID: int64(actor), UserIds: invited})
	if err != nil {
		return fmt.Errorf("channels: check group friendship: %w", err)
	}
	if len(friends) != len(invited) {
		return notFriend()
	}
	return nil
}

func notFriend() error {
	return httpx.BadRequest("not_a_friend", "group recipients must be your friends")
}

// checkGroupCapacity counts existing memberships while their accounts are locked.
func (s *Service) checkGroupCapacity(ctx context.Context, tx pgx.Tx, users []int64) error {
	full, err := s.q.GroupsAtCapacity(ctx, tx, store.GroupsAtCapacityParams{UserIds: users, MaxGroups: maxGroupsPerUser})
	if err != nil {
		return fmt.Errorf("channels: count group memberships: %w", err)
	}
	if full {
		return httpx.BadRequest("too_many_groups", fmt.Sprintf("a user can be in at most %d group DMs", maxGroupsPerUser))
	}
	return nil
}

// AddGroupRecipient adds the caller's friend and starts their read marker
// at the current head. Existing members are idempotent even at capacity.
func (s *Service) AddGroupRecipient(ctx context.Context, id, actor, target snowflake.ID) error {
	return s.privateWrite(ctx, id, actor, func(tx pgx.Tx, before privateSnapshot) ([]events.Event, error) {
		if err := before.requireGroup(); err != nil {
			return nil, err
		}
		if before.hasMember(target) {
			return nil, nil
		}
		if len(before.members) >= maxGroupRecipients {
			return nil, httpx.BadRequest("group_full", fmt.Sprintf("a group can have at most %d recipients", maxGroupRecipients))
		}
		ids := []int64{int64(target)}
		if err := s.groupCandidates(ctx, tx, actor, []int64{int64(actor), int64(target)}, ids); err != nil {
			return nil, err
		}
		if err := s.checkGroupCapacity(ctx, tx, ids); err != nil {
			return nil, err
		}
		if err := s.q.AddPrivateRecipients(ctx, tx, store.AddPrivateRecipientsParams{
			ChannelID: int64(id), UserIds: ids, JoinedAt: s.now(),
		}); err != nil {
			return nil, fmt.Errorf("channels: add group recipient: %w", err)
		}
		if err := s.q.SeedReadStates(ctx, tx, store.SeedReadStatesParams{UserID: int64(target), ChannelIds: []int64{int64(id)}}); err != nil {
			return nil, fmt.Errorf("channels: seed group read marker: %w", err)
		}
		after, err := s.privateSnapshot(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return append([]events.Event{after.event(events.DMChannelCreate, target)}, after.groupEvents(events.DMChannelUpdate)...), nil
	})
}

// RemoveGroupRecipient lets members leave and the owner remove others.
func (s *Service) RemoveGroupRecipient(ctx context.Context, id, actor, target snowflake.ID) error {
	return s.privateWrite(ctx, id, actor, func(tx pgx.Tx, before privateSnapshot) ([]events.Event, error) {
		if err := before.requireGroup(); err != nil {
			return nil, err
		}
		if target != actor && (before.row.OwnerID == nil || *before.row.OwnerID != actor) {
			return nil, ErrMissingPerm
		}
		if !before.hasMember(target) {
			return nil, nil
		}
		if err := s.q.RemovePrivateRecipient(ctx, tx, store.RemovePrivateRecipientParams{ChannelID: int64(id), UserID: int64(target)}); err != nil {
			return nil, fmt.Errorf("channels: remove group recipient: %w", err)
		}
		announce := []events.Event{privateDelete(id, target)}
		// The recipient trigger deletes empty groups or reassigns ownership.
		if len(before.members) == 1 {
			return announce, nil
		}
		after, err := s.privateSnapshot(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return append(announce, after.groupEvents(events.DMChannelUpdate)...), nil
	})
}

// RenameGroup changes only the optional group name.
func (s *Service) RenameGroup(ctx context.Context, id, actor snowflake.ID, name *string) (DMChannel, error) {
	var out DMChannel
	err := s.privateWrite(ctx, id, actor, func(tx pgx.Tx, before privateSnapshot) ([]events.Event, error) {
		if err := before.requireGroup(); err != nil {
			return nil, err
		}
		if name != nil {
			clean, err := normalizeName(*name)
			if err != nil {
				return nil, err
			}
			name = &clean
		}
		prev := before.row.Name
		if (prev == nil && name == nil) || (prev != nil && name != nil && *prev == *name) {
			out = before.view(actor)
			return nil, nil
		}
		if err := s.q.RenamePrivateGroup(ctx, tx, store.RenamePrivateGroupParams{ID: id, Name: name}); err != nil {
			return nil, fmt.Errorf("channels: rename group: %w", err)
		}
		before.row.Name = name
		out = before.view(actor)
		return before.groupEvents(events.DMChannelUpdate), nil
	})
	return out, err
}

func (p privateSnapshot) requireGroup() error {
	if Type(p.row.Type) != TypeGroupDM {
		return invalid("type", "must be a group DM")
	}
	return nil
}

func (p privateSnapshot) hasMember(id snowflake.ID) bool {
	return slices.ContainsFunc(p.members, func(member auth.PublicUser) bool { return member.ID == id })
}

// groupEvents builds the same event once per recipient with the relative list.
func (p privateSnapshot) groupEvents(typ string) []events.Event {
	out := make([]events.Event, 0, len(p.members))
	for _, member := range p.members {
		out = append(out, p.event(typ, member.ID))
	}
	return out
}
