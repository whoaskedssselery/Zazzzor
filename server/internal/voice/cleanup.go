package voice

import (
	"context"
	"slices"

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
		s.membersRemoved[memberKey{guildID: guildID, userID: userID}] = s.invalidationLocked()
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

// RemoveChannel takes everyone out of a deleted voice channel.
func (s *Service) RemoveChannel(ctx context.Context, channelID snowflake.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRemovalsLocked()
	s.rev++
	s.channelsRemoved[channelID] = s.invalidationLocked()
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
