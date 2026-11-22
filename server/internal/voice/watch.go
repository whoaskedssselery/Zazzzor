package voice

import (
	"context"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// WatchPayload is the data of STREAM_WATCH: who started or stopped
// watching the receiver's screen share in channel ChannelID.
type WatchPayload struct {
	ChannelID snowflake.ID `json:"channel_id"`
	UserID    snowflake.ID `json:"user_id"`
	Watching  bool         `json:"watching"`
}

// WatchStream tells streamerID that viewerID started (watching) or stopped
// watching their screen share, so their client can sound and count it, as
// Discord does. Both must be in channelID, and streamerID must be sharing a
// screen there. Nothing is kept: who watches lives in the sharer's client,
// which forgets a viewer who leaves the call.
func (s *Service) WatchStream(ctx context.Context, channelID, viewerID, streamerID snowflake.ID, watching bool) error {
	if !s.enabled {
		return ErrUnavailable
	}
	if viewerID == streamerID {
		return invalid("user_id", "cannot watch yourself")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	viewer := s.sessions[viewerID]
	if viewer == nil || viewer.state.ChannelID != channelID {
		return ErrNotInVoice
	}
	streamer := s.sessions[streamerID]
	if streamer == nil || streamer.state.ChannelID != channelID || !streamer.state.SelfStream {
		return ErrNotStreaming
	}
	if s.pub != nil {
		s.pub.Publish(ctx, events.Event{Type: events.StreamWatch,
			Audience: events.Audience{Kind: events.AudienceUsers, GuildID: viewer.state.GuildID, ChannelID: channelID,
				UserIDs: []snowflake.ID{streamerID}},
			Payload: WatchPayload{ChannelID: channelID, UserID: viewerID, Watching: watching}})
	}
	return nil
}
