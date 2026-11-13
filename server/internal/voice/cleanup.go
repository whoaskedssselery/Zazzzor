package voice

import (
	"context"
	"slices"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// The removals below follow committed changes in other modules; they
// clear the states at once, announce the leaves and queue the removals
// from LiveKit for Run. They never call LiveKit themselves, so they may be
// called from Publish.

// RemoveMembers takes userIDs, who are no longer members of guildID (they
// left, were kicked or banned, or the guild is gone), out of its voice
// channels. Their states in other guilds stay.
func (s *Service) RemoveMembers(ctx context.Context, guildID snowflake.ID, userIDs []snowflake.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRemovalsLocked()
	for _, userID := range userIDs {
		if move := s.moves[userID]; move != nil && move.source.state.GuildID == guildID {
			delete(s.moves, userID)
		}
		s.rev++
		key := memberKey{guildID: guildID, userID: userID}
		s.membersRemoved[key] = s.invalidationLocked()
		// Server mute and deafen belong to the membership that just ended.
		delete(s.moderation, key)
		if s.intents[userID].guildID == guildID {
			s.intents[userID] = intent{}
		}
	}
	for userID, sess := range s.sessions {
		if sess.state.GuildID == guildID && slices.Contains(userIDs, userID) {
			s.dropLocked(ctx, userID, true)
		}
	}
}

// RemoveChannel takes everyone out of a deleted voice channel. Pending
// moves into it are cancelled: their participants stay in the source.
func (s *Service) RemoveChannel(ctx context.Context, channelID snowflake.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRemovalsLocked()
	s.rev++
	s.channelsRemoved[channelID] = s.invalidationLocked()
	for userID, move := range s.moves {
		if move.channelID == channelID {
			s.cancelMoveLocked(ctx, userID)
		}
	}
	// Defence in depth: admission also rejects deleted channels.
	for userID, pending := range s.intents {
		if pending.channelID == channelID {
			s.intents[userID] = intent{}
			s.touchLocked(userID)
		}
	}
	for userID, sess := range s.sessions {
		if sess.state.ChannelID == channelID {
			s.dropLocked(ctx, userID, true)
		}
	}
}

// RemoveUser takes userID out of voice everywhere: their state, any join
// still pending and every call ringing them (an account disabled by an
// admin). Their LiveKit participant is removed by Run.
func (s *Service) RemoveUser(ctx context.Context, userID snowflake.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRemovalsLocked()
	s.rev++
	s.revokeLocked(ctx, userID, func(snowflake.ID) bool { return true })
	// The zero intent refuses every join generation, also when the service
	// knew of none (it restarted), so a webhook already in flight cannot
	// bring the user back.
	s.intents[userID] = intent{}
}

// RecheckCalls takes each of userIDs out of the private calls they may no
// longer be in (a block between the two of a 1:1): their state there, a
// pending join and the ringing. It reads the database, so callers run it
// off the publishing goroutine.
func (s *Service) RecheckCalls(ctx context.Context, userIDs []snowflake.ID) {
	if s.private == nil {
		return
	}
	for _, userID := range userIDs {
		s.mu.Lock()
		var channelIDs []snowflake.ID
		if sess := s.sessions[userID]; sess != nil && sess.state.GuildID == 0 {
			channelIDs = append(channelIDs, sess.state.ChannelID)
		}
		if in := s.intents[userID]; in.guildID == 0 && in.channelID != 0 {
			channelIDs = append(channelIDs, in.channelID)
		}
		for channelID, c := range s.calls {
			if _, ok := c.ringing[userID]; ok {
				channelIDs = append(channelIDs, channelID)
			}
		}
		s.mu.Unlock()
		refused := map[snowflake.ID]bool{}
		for _, channelID := range channelIDs {
			if _, done := refused[channelID]; done {
				continue
			}
			may, err := s.private.MayCall(ctx, channelID, userID)
			if err != nil {
				s.log.Warn("voice: recheck call", "channel_id", channelID.String(), "err", err.Error())
				continue
			}
			refused[channelID] = !may
		}
		s.mu.Lock()
		s.rev++
		s.revokeLocked(ctx, userID, func(channelID snowflake.ID) bool { return refused[channelID] })
		s.mu.Unlock()
	}
}

// revokeLocked takes userID out of the private calls and voice channels
// for which gone reports true: the ringing, a pending join, the state.
func (s *Service) revokeLocked(ctx context.Context, userID snowflake.ID, gone func(channelID snowflake.ID) bool) {
	for channelID, c := range s.calls {
		if _, ok := c.ringing[userID]; ok && gone(channelID) {
			delete(c.ringing, userID)
			if !s.endCallIfIdleLocked(ctx, channelID) {
				s.publishCallLocked(ctx, events.CallUpdate, channelID, c.public(channelID))
			}
		}
	}
	if in := s.intents[userID]; in.channelID != 0 && gone(in.channelID) {
		s.intents[userID] = intent{}
		s.touchLocked(userID)
	}
	if move := s.moves[userID]; move != nil && gone(move.channelID) {
		s.cancelMoveLocked(ctx, userID)
	}
	if sess := s.sessions[userID]; sess != nil && gone(sess.state.ChannelID) {
		s.dropLocked(ctx, userID, true)
	}
}
