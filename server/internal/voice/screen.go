package voice

import (
	"context"
	"fmt"
	"strings"

	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// screenOwner accepts only a canonical user ID with the companion suffix.
func screenOwner(identity string) (snowflake.ID, bool) {
	owner, ok := strings.CutSuffix(identity, ".screen")
	if !ok {
		return 0, false
	}
	return identityUser(owner)
}

// screenPermissions permits screen video and audio without receiving media.
func screenPermissions() ParticipantPermissions {
	return ParticipantPermissions{Sources: []livekit.TrackSource{
		livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO,
	}}
}

// JoinScreen issues a publishing-only companion token for the active session.
// It does not create a call, change its ringing or supersede the primary token.
func (s *Service) JoinScreen(ctx context.Context, channelID snowflake.ID, user auth.User, origin string) (Join, error) {
	if !s.enabled {
		return Join{}, ErrUnavailable
	}
	unlock, err := s.lockUser(ctx, user.ID)
	if err != nil {
		return Join{}, err
	}
	defer unlock()
	s.mu.Lock()
	s.expireMovesLocked(ctx)
	captured := s.sessions[user.ID]
	active := captured != nil && captured.state.ChannelID == channelID
	s.mu.Unlock()
	if !active {
		return Join{}, ErrNotInVoice
	}
	_, p, admitted, err := s.admitted(ctx, channelID, Participant{Identity: user.ID.String()})
	if err != nil {
		return Join{}, fmt.Errorf("voice: read screen token permissions: %w", err)
	}
	if !admitted || !p.Has(permissions.Stream) {
		return Join{}, ErrMissingPerm
	}
	signaling, err := signalingURL(origin)
	if err != nil {
		return Join{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[user.ID] != captured {
		return Join{}, ErrNotInVoice
	}
	g := &lkauth.VideoGrant{RoomJoin: true, Room: channelID.String()}
	g.SetCanSubscribe(false)
	g.SetCanPublish(true)
	g.SetCanPublishSources(screenPermissions().Sources)
	g.SetCanPublishData(false)
	g.SetCanUpdateOwnMetadata(false)
	token, err := s.signToken(user.ID.String()+".screen", user.DisplayName, g, captured.join)
	if err != nil {
		return Join{}, err
	}
	return Join{Token: token, URL: signaling}, nil
}

