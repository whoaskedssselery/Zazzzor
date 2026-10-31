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
	for userID, sess := range s.sessions {
		if sess.state.ChannelID == channelID {
			s.dropLocked(ctx, userID, true)
		}
	}
}
