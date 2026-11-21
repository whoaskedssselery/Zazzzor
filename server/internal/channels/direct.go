package channels

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// cannotMessage deliberately uses one problem for every eligibility failure.
func cannotMessage() error {
	return &httpx.Problem{Status: http.StatusForbidden, Code: "cannot_message_user",
		Detail: "Could not send: you share no server, you are not friends, or this person limits direct messages."}
}

// directAllowed locks the same account rows as relationship mutations.
func (s *Service) directAllowed(ctx context.Context, tx pgx.Tx, actor, other snowflake.ID) error {
	if actor <= 0 || other <= 0 || actor == other {
		return cannotMessage()
	}
	accounts, err := s.q.LockPrivateAccounts(ctx, tx, []int64{int64(actor), int64(other)})
	if err != nil {
		return fmt.Errorf("channels: lock direct recipients: %w", err)
	}
	if len(accounts) != 2 {
		return cannotMessage()
	}
	allowed, err := s.q.CanDirectMessage(ctx, tx, store.CanDirectMessageParams{ActorID: int64(actor), OtherID: int64(other)})
	if err != nil {
		return fmt.Errorf("channels: check direct message eligibility: %w", err)
	}
	if !allowed.Valid || !allowed.Bool {
		return cannotMessage()
	}
	return nil
}

// OpenDirect creates or reopens the unique channel for an eligible pair.
func (s *Service) OpenDirect(ctx context.Context, actor, other snowflake.ID) (DMChannel, bool, error) {
	if ok, retry := s.directLimiter.Allow(actor.String()); !ok {
		return DMChannel{}, false, httpx.TooManyRequests(retry)
	}
	if actor == other || other <= 0 {
		return DMChannel{}, false, cannotMessage()
	}
	low, high := min(actor, other), max(actor, other)
	key := int64(low) ^ int64(high) ^ low.Time().UnixMilli() ^ high.Time().UnixMilli()
	mu := &s.directLocks[key&int64(len(s.directLocks)-1)]
	mu.Lock()
	defer mu.Unlock()
	pair := store.DirectChannelOfPairParams{LowID: int64(low), HighID: int64(high)}
	existing, pairErr := s.q.DirectChannelOfPair(ctx, s.pool, pair)
	if pairErr != nil && !errors.Is(pairErr, pgx.ErrNoRows) {
		return DMChannel{}, false, fmt.Errorf("channels: find direct pair: %w", pairErr)
	}
	id := snowflake.ID(existing)
	if id == 0 {
		id = s.ids.Next()
	}
	defer s.LockPrivateEvents(id)()

	var snapshot privateSnapshot
	var announce []events.Event
	var created bool
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.q.LockDirectPair(ctx, tx, store.LockDirectPairParams(pair)); err != nil {
			return fmt.Errorf("channels: serialize direct open: %w", err)
		}
		current, err := s.q.DirectChannelOfPair(ctx, tx, pair)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("channels: reload direct pair: %w", err)
		}
		created, announce = errors.Is(err, pgx.ErrNoRows), nil
		if !created {
			id = snowflake.ID(current)
			if err = s.LockPrivateIn(ctx, tx, id); err != nil {
				return err
			}
		}
		if err = s.directAllowed(ctx, tx, actor, other); err != nil {
			return err
		}
		if created {
			if err = s.createDirectIn(ctx, tx, id, actor, other, pair); err != nil {
				return err
			}
		}
		n, err := s.q.SetDirectClosed(ctx, tx, store.SetDirectClosedParams{ChannelID: int64(id), UserID: int64(actor)})
		if err != nil {
			return fmt.Errorf("channels: reopen direct channel: %w", err)
		}
		snapshot, err = s.privateSnapshot(ctx, tx, id)
		if err != nil {
			return err
		}
		if created || n != 0 {
			announce = append(announce, snapshot.event(events.DMChannelCreate, actor))
		}
		if created {
			announce = append(announce, snapshot.event(events.DMChannelCreate, other))
		}
		return nil
	})
	if err != nil {
		return DMChannel{}, false, fmt.Errorf("channels: open direct channel: %w", err)
	}
	for _, event := range announce {
		s.publish(ctx, event)
	}
	return snapshot.view(actor), created, nil
}

func (s *Service) createDirectIn(ctx context.Context, tx pgx.Tx, id, actor, other snowflake.ID, pair store.DirectChannelOfPairParams) error {
	now := s.now()
	if _, err := s.q.CreatePrivateChannel(ctx, tx, store.CreatePrivateChannelParams{ID: id, Type: int16(TypeDM), CreatedAt: now}); err != nil {
		return fmt.Errorf("channels: insert direct channel: %w", err)
	}
	if err := s.q.BindDirectPair(ctx, tx, store.BindDirectPairParams{LowID: pair.LowID, HighID: pair.HighID, ChannelID: int64(id)}); err != nil {
		return fmt.Errorf("channels: bind direct pair: %w", err)
	}
	if err := s.q.AddPrivateRecipients(ctx, tx, store.AddPrivateRecipientsParams{ChannelID: int64(id), UserIds: []int64{int64(actor), int64(other)}, JoinedAt: now}); err != nil {
		return fmt.Errorf("channels: insert direct recipients: %w", err)
	}
	return nil
}

// CheckPrivateSendIn enforces the 1:1 eligibility rule under the channel lock.
func (s *Service) CheckPrivateSendIn(ctx context.Context, tx pgx.Tx, id, actor snowflake.ID) error {
	row, err := s.q.ChannelByID(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("channels: load private send target: %w", err)
	}
	if row.Type == int16(TypeGroupDM) {
		return nil
	}
	if row.Type != int16(TypeDM) {
		return ErrNotFound
	}
	ids, err := s.PrivateRecipientsIn(ctx, tx, id)
	if err != nil {
		return err
	}
	if len(ids) != 2 {
		return cannotMessage()
	}
	if ids[0] == actor {
		return s.directAllowed(ctx, tx, actor, ids[1])
	}
	if ids[1] == actor {
		return s.directAllowed(ctx, tx, actor, ids[0])
	}
	return ErrNotFound
}

// ReopenPrivateIn opens closed 1:1 views and prepares events to send before the message.
func (s *Service) ReopenPrivateIn(ctx context.Context, tx pgx.Tx, id snowflake.ID) ([]events.Event, error) {
	users, err := s.q.ReopenPrivateRecipients(ctx, tx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("channels: reopen private recipients: %w", err)
	}
	if len(users) == 0 {
		return nil, nil
	}
	snapshot, err := s.privateSnapshot(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	out := make([]events.Event, 0, len(users))
	for _, user := range users {
		out = append(out, snapshot.event(events.DMChannelCreate, snowflake.ID(user)))
	}
	return out, nil
}

// PrepareCall checks that userID may start or join the call of private
// channel id: a recipient, and in a 1:1 on the terms of a message. A closed
// 1:1 reopens for the other recipient, so the call that rings them shows.
// It returns every recipient.
func (s *Service) PrepareCall(ctx context.Context, id, userID snowflake.ID) ([]snowflake.ID, error) {
	var recipients []snowflake.ID
	err := s.privateWrite(ctx, id, userID, func(tx pgx.Tx, snapshot privateSnapshot) ([]events.Event, error) {
		if err := s.CheckPrivateSendIn(ctx, tx, id, userID); err != nil {
			return nil, err
		}
		// Nobody is rung by someone they blocked or who blocked them; in a
		// group they still share the call if they join it themselves.
		blocked, err := s.q.BlockedWith(ctx, tx, int64(userID))
		if err != nil {
			return nil, fmt.Errorf("channels: load blocks of caller: %w", err)
		}
		for _, m := range snapshot.members {
			if !slices.Contains(blocked, int64(m.ID)) {
				recipients = append(recipients, m.ID)
			}
		}
		return s.ReopenPrivateIn(ctx, tx, id)
	})
	if err != nil {
		return nil, err
	}
	return recipients, nil
}

// MayCall reports whether userID may be in the call of private channel id
// now: a recipient, and in a 1:1 on the terms of a message, the rule
// PrepareCall applies to a new token. Joins and reconciliation check it
// again, so a block or a lost shared server ends a call already started.
func (s *Service) MayCall(ctx context.Context, id, userID snowflake.ID) (bool, error) {
	var ok bool
	err := s.read(ctx, func(tx pgx.Tx) error {
		row, err := s.q.ChannelByID(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("channels: load call channel: %w", err)
		}
		if !Type(row.Type).Private() {
			return nil
		}
		ids, err := s.PrivateRecipientsIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(ids, userID) {
			return nil
		}
		if Type(row.Type) == TypeGroupDM {
			ok = true
			return nil
		}
		if len(ids) != 2 {
			return nil
		}
		other := ids[0]
		if other == userID {
			other = ids[1]
		}
		allowed, err := s.q.CanDirectMessage(ctx, tx, store.CanDirectMessageParams{ActorID: int64(userID), OtherID: int64(other)})
		if err != nil {
			return fmt.Errorf("channels: check call eligibility: %w", err)
		}
		ok = allowed.Valid && allowed.Bool
		return nil
	})
	return ok, err
}
