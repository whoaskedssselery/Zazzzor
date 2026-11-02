package voice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// MovePayload asks only the participant to fetch a token and reconnect.
type MovePayload struct {
	ChannelID snowflake.ID `json:"channel_id"`
}

// pendingMove pins the source until destination admission or the deadline.
// prior is the user's intent when the move was requested (none: hadPrior is
// false); cancelling restores it if a destination token was issued since.
type pendingMove struct {
	source    *session
	channelID snowflake.ID
	deadline  time.Time
	prior     intent
	hadPrior  bool
}

// checkDestination checks both the actor's MOVE and the target's CONNECT.
func (s *Service) checkDestination(ctx context.Context, guildID, actorID, userID, channelID snowflake.ID) error {
	if channelID <= 0 {
		return invalid("channel_id", "must be a voice channel ID or null")
	}
	p, gid, err := s.channels.Permissions(ctx, actorID, channelID)
	if err != nil {
		return fmt.Errorf("voice: read destination moderator permissions: %w", err)
	}
	if gid != guildID {
		return channels.ErrNotFound
	}
	if !p.Has(permissions.MoveMembers) {
		return ErrMissingPerm
	}
	c, err := s.channels.Lookup(ctx, channelID)
	if err != nil {
		return fmt.Errorf("voice: read move destination: %w", err)
	}
	// Defence in depth: the actor permission read already checked this guild.
	if c.GuildID != guildID {
		return channels.ErrNotFound
	}
	if c.Type != channels.TypeVoice {
		return ErrNotVoiceChannel
	}
	p, gid, err = s.channels.Permissions(ctx, userID, channelID)
	if errors.Is(err, channels.ErrNotFound) {
		return ErrMissingPerm
	}
	if err != nil {
		return fmt.Errorf("voice: read destination participant permissions: %w", err)
	}
	if gid != guildID || !p.Has(permissions.Connect) {
		return ErrMissingPerm
	}
	return nil
}

// requestMoveLocked publishes a new private handoff without extending an
// identical pending request. Older tokens cannot complete a superseded move.
// A move back to the source cancels the pending move; with none pending it
// does nothing.
func (s *Service) requestMoveLocked(ctx context.Context, userID, channelID snowflake.ID, source *session) {
	old := s.moves[userID]
	if channelID == source.state.ChannelID {
		if old != nil && old.source == source {
			s.cancelMoveLocked(ctx, userID)
		}
		return
	}
	if old != nil && old.source == source && old.channelID == channelID {
		return
	}
	move := &pendingMove{source: source, channelID: channelID, deadline: s.now().Add(MoveTimeout)}
	move.prior, move.hadPrior = s.intents[userID]
	if old != nil && old.source == source {
		move.prior, move.hadPrior = old.prior, old.hadPrior
	}
	s.moves[userID] = move
	s.touchLocked(userID)
	s.publishMoveLocked(ctx, source.state.GuildID, userID, channelID)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// cancelMoveLocked drops userID's pending move: the source stays, a
// destination token issued for it no longer connects, and the participant
// is sent back to the source, in case their client already left it.
func (s *Service) cancelMoveLocked(ctx context.Context, userID snowflake.ID) {
	move := s.moves[userID]
	if move == nil {
		return
	}
	delete(s.moves, userID)
	// Every hop of a chained move issued its own token; the latest join is
	// again the one before the move, whichever hop the intent points at.
	if move.hadPrior {
		s.intents[userID] = move.prior
	} else {
		delete(s.intents, userID)
	}
	s.touchLocked(userID)
	s.publishMoveLocked(ctx, move.source.state.GuildID, userID, move.source.state.ChannelID)
}

// publishMoveLocked sends VOICE_MOVE to the participant alone; the gateway
// drops it unless they may view channelID.
func (s *Service) publishMoveLocked(ctx context.Context, guildID, userID, channelID snowflake.ID) {
	if s.pub == nil {
		return
	}
	s.pub.Publish(ctx, events.Event{Type: events.VoiceMove,
		Audience: events.Audience{Kind: events.AudienceUsers, GuildID: guildID, ChannelID: channelID, UserIDs: []snowflake.ID{userID}},
		Payload:  MovePayload{ChannelID: channelID}})
}

// currentParticipantLocked keeps exactly the pinned source alive while a
// pending move permits only fresh tokens for its current destination.
func (s *Service) currentParticipantLocked(userID, channelID snowflake.ID, p Participant) bool {
	if move := s.moves[userID]; move != nil {
		if channelID == move.source.state.ChannelID && p.SID == move.source.sid {
			return true
		}
		if channelID != move.channelID {
			return false
		}
	}
	return s.currentLocked(userID, channelID, p.Join)
}

// expireMovesLocked queues only the captured source and returns the next
// deadline. An already-issued destination token may complete after expiry.
func (s *Service) expireMovesLocked(ctx context.Context) time.Time {
	now := s.now()
	var next time.Time
	for userID, move := range s.moves {
		if move.deadline.After(now) {
			if next.IsZero() || move.deadline.Before(next) {
				next = move.deadline
			}
			continue
		}
		delete(s.moves, userID)
		if s.intents[userID].channelID != move.channelID {
			s.intents[userID] = intent{}
		}
		if s.sessions[userID] == move.source {
			s.dropLocked(ctx, userID, false)
		} else {
			s.touchLocked(userID)
		}
		s.removeLocked(move.source.room, userID.String(), move.source.sid)
	}
	return next
}

// disconnect removes the captured participant synchronously. A left
// webhook caused by that RPC may already have cleared the local state.
func (s *Service) disconnect(ctx context.Context, guildID, userID snowflake.ID, source *session, flags serverFlags, start uint64) error {
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	err := s.rooms.RemoveParticipant(rpcCtx, source.room, userID.String())
	cancel()
	if err != nil {
		return fmt.Errorf("%w: voice: disconnect participant: %w", ErrUnavailable, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.sessions[userID]
	if s.invalidatedLocked(guildID, userID, source.state.ChannelID, start) {
		return nil
	}
	if current != nil && current != source {
		s.commitModerationLocked(ctx, memberKey{guildID: guildID, userID: userID}, flags)
		// The identity RPC addressed the captured room, never this new call.
		if current.state.GuildID == guildID {
			s.queueRefreshLocked(guildID)
		}
		return nil
	}
	s.storeModerationLocked(memberKey{guildID: guildID, userID: userID}, flags)
	s.intents[userID] = intent{}
	delete(s.moves, userID)
	if current != nil {
		current.state.Mute, current.state.Deaf = flags.mute, flags.deaf
		s.dropLocked(ctx, userID, false)
	} else {
		s.touchLocked(userID)
	}
	return nil
}
