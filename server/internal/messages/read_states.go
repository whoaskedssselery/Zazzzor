package messages

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// ReadState is a user's read position in one channel and its outstanding mentions.
type ReadState struct {
	ChannelID    snowflake.ID `json:"channel_id"`
	LastReadID   snowflake.ID `json:"last_read_id"`
	MentionCount int32        `json:"mention_count"`
}

// readState converts the database row without exposing the account ID.
func readState(row store.ReadState) ReadState {
	return ReadState{ChannelID: snowflake.ID(row.ChannelID), LastReadID: snowflake.ID(row.LastReadID),
		MentionCount: row.MentionCount}
}

// ReadStates returns the user's markers in channelIDs, which the caller has
// already filtered to channels the user may see.
func (s *Service) ReadStates(ctx context.Context, userID snowflake.ID, channelIDs []snowflake.ID) ([]ReadState, error) {
	if len(channelIDs) == 0 {
		return []ReadState{}, nil
	}
	keys := make([]int64, len(channelIDs))
	for i, id := range channelIDs {
		keys[i] = int64(id)
	}
	rows, err := s.q.ReadStatesForChannels(ctx, s.pool, store.ReadStatesForChannelsParams{UserID: int64(userID), ChannelIds: keys})
	if err != nil {
		return nil, fmt.Errorf("messages: read channel markers: %w", err)
	}
	out := make([]ReadState, len(rows))
	for i, row := range rows {
		out[i] = readState(row)
	}
	return out, nil
}

// withGuildWrite serializes message and marker changes with visibility and membership updates.
func (s *Service) withGuildWrite(ctx context.Context, guildID snowflake.ID, change func(pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.channels.LockGuild(ctx, tx, guildID); err != nil {
			if errors.Is(err, guilds.ErrNotMember) {
				return channels.ErrNotFound
			}
			return fmt.Errorf("messages: lock guild: %w", err)
		}
		return change(tx)
	})
}

// Ack advances a channel marker only after verifying current access and the target.
func (s *Service) Ack(ctx context.Context, channelID, messageID, userID snowflake.ID) error {
	_, guildID, err := s.access(ctx, userID, channelID, permissions.ReadMessageHistory)
	if err != nil {
		return fmt.Errorf("messages: acknowledge channel: %w", err)
	}
	var changed []store.ReadState
	err = s.withChannelWrite(ctx, guildID, channelID, func(tx pgx.Tx) error {
		p, _, accessErr := s.channels.PermissionsIn(ctx, tx, userID, channelID)
		if accessErr != nil {
			return fmt.Errorf("messages: check acknowledgment access: %w", accessErr)
		}
		if !p.Has(permissions.ReadMessageHistory) {
			return ErrMissingPerm
		}
		_, targetErr := s.q.MessageInChannel(ctx, tx, store.MessageInChannelParams{ID: messageID, ChannelID: channelID})
		if errors.Is(targetErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if targetErr != nil {
			return fmt.Errorf("messages: read acknowledgment target: %w", targetErr)
		}
		var advanceErr error
		changed, advanceErr = s.advanceRead(ctx, tx, guildID, userID, []int64{int64(channelID)}, []int64{int64(messageID)})
		return advanceErr
	})
	if err != nil {
		return fmt.Errorf("messages: acknowledge message: %w", err)
	}
	s.publishAcks(ctx, userID, changed)
	return nil
}

// AckGuild marks every visible nonempty channel read in one transaction.
func (s *Service) AckGuild(ctx context.Context, guildID, userID snowflake.ID) error {
	var changed []store.ReadState
	err := s.withGuildWrite(ctx, guildID, func(tx pgx.Tx) error {
		visible, err := s.channels.VisibleChannelsIn(ctx, tx, userID, guildID)
		if errors.Is(err, guilds.ErrNotMember) || errors.Is(err, guilds.ErrNotFound) {
			return channels.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("messages: list channels for acknowledgment: %w", err)
		}
		changed, err = s.ackVisible(ctx, tx, userID, guildID, visible)
		return err
	})
	if err != nil {
		return fmt.Errorf("messages: acknowledge guild: %w", err)
	}
	s.publishAcks(ctx, userID, changed)
	return nil
}

// ackVisible clears stale counters even if deletions moved a channel's head
// behind the user's marker. An untouched empty channel has nothing to ack.
func (s *Service) ackVisible(ctx context.Context, tx pgx.Tx, userID, guildID snowflake.ID, visible []channels.Channel) ([]store.ReadState, error) {
	visibleIDs := make([]int64, 0, len(visible))
	for _, channel := range visible {
		if channel.Type == channels.TypeText {
			visibleIDs = append(visibleIDs, int64(channel.ID))
		}
	}
	states, err := s.q.ReadStatesForChannels(ctx, tx, store.ReadStatesForChannelsParams{UserID: int64(userID), ChannelIds: visibleIDs})
	if err != nil {
		return nil, fmt.Errorf("messages: read markers for guild acknowledgment: %w", err)
	}
	previous := make(map[snowflake.ID]store.ReadState, len(states))
	for _, row := range states {
		previous[snowflake.ID(row.ChannelID)] = row
	}
	channelIDs, messageIDs := make([]int64, 0, len(visibleIDs)), make([]int64, 0, len(visibleIDs))
	for _, channel := range visible {
		row, hasState := previous[channel.ID]
		if channel.LastMessageID == nil && !hasState {
			continue
		}
		target := row.LastReadID
		if channel.LastMessageID != nil {
			target = max(target, int64(*channel.LastMessageID))
		}
		channelIDs = append(channelIDs, int64(channel.ID))
		messageIDs = append(messageIDs, target)
	}
	return s.advanceRead(ctx, tx, guildID, userID, channelIDs, messageIDs)
}

// advanceRead uses the same monotonic update for sends and explicit acknowledgments.
func (s *Service) advanceRead(ctx context.Context, tx pgx.Tx, guildID, userID snowflake.ID,
	channelIDs, messageIDs []int64,
) ([]store.ReadState, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	rows, err := s.q.AdvanceReadStates(ctx, tx, store.AdvanceReadStatesParams{UserID: int64(userID), GuildID: dbGuildID(guildID),
		ChannelIds: channelIDs, MessageIds: messageIDs})
	if err != nil {
		return nil, fmt.Errorf("messages: advance read markers: %w", err)
	}
	return rows, nil
}

// publishAcks sends each changed marker only to the user's connections.
func (s *Service) publishAcks(ctx context.Context, userID snowflake.ID, changed []store.ReadState) {
	for _, row := range changed {
		s.publish(ctx, events.Event{Type: events.MessageAck,
			Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{userID}},
			Payload:  readState(row)})
	}
}
