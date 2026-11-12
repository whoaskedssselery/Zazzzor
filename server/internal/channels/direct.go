package channels

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
		Detail: "Не получилось отправить: у вас нет общего сервера, вы не друзья или этот человек ограничил личные сообщения."}
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
