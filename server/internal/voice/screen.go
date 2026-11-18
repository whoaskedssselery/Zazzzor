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

// screenObservation pins a reconcile listing to the session revision it observed.
type screenObservation struct {
	sess     *session
	revision uint64
}

// joinedScreen admits a companion only while its captured owner remains current.
// The optional observation prevents a stale listing from replacing newer state.
func (s *Service) joinedScreen(ctx context.Context, channelID snowflake.ID, room Room, p Participant, observed *screenObservation) error {
	userID, _ := screenOwner(p.Identity)
	unlock, err := s.lockUser(ctx, userID)
	if err != nil {
		return err
	}
	defer unlock()
	s.mu.Lock()
	s.expireMovesLocked(ctx)
	captured, start, epoch := s.sessions[userID], s.rev, s.permissionRev
	if observed != nil && (captured != observed.sess || s.revs[userID] != observed.revision) {
		s.mu.Unlock()
		return nil
	}
	valid := s.screenCurrentLocked(captured, room, p)
	if !valid {
		s.removeLocked(room.Name, p.Identity, p.SID)
	}
	s.mu.Unlock()
	if !valid {
		return nil
	}
	_, mask, admitted, err := s.admitted(ctx, channelID, Participant{Identity: userID.String()})
	if err != nil {
		return fmt.Errorf("voice: read companion permissions: %w", err)
	}
	s.mu.Lock()
	valid = admitted && mask.Has(permissions.Stream) && s.sessions[userID] == captured &&
		!s.invalidatedLocked(captured.state.GuildID, userID, channelID, start) && s.screenCurrentLocked(captured, room, p)
	if !valid {
		s.removeLocked(room.Name, p.Identity, p.SID)
	}
	s.mu.Unlock()
	if !valid {
		return nil
	}
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	err = s.rooms.UpdateParticipant(rpcCtx, room.Name, p.Identity, screenPermissions())
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.removeLocked(room.Name, p.Identity, p.SID)
		return fmt.Errorf("voice: enforce companion permissions: %w", err)
	}
	if s.sessions[userID] != captured || !s.screenCurrentLocked(captured, room, p) ||
		s.invalidatedLocked(captured.state.GuildID, userID, channelID, start) {
		s.removeLocked(room.Name, p.Identity, p.SID)
		return nil
	}
	s.cancelRemovalLocked(removalKey{room: room.Name, identity: p.Identity}, p.SID)
	if captured.companion != nil && captured.companion.p.SID != p.SID {
		s.removeLocked(room.Name, p.Identity, captured.companion.p.SID)
	}
	tracks := map[string]bool{}
	for _, sid := range p.Screens {
		tracks[sid] = true
	}
	captured.companion = &screenSession{p: p, screens: tracks}
	s.touchLocked(userID)
	s.syncStreamLocked(ctx, captured)
	if s.permissionRev != epoch {
		s.queueRefreshLocked(captured.state.GuildID)
	}
	return nil
}

// screenCurrentLocked checks the primary binding and departed/replaced SIDs.
func (s *Service) screenCurrentLocked(sess *session, room Room, p Participant) bool {
	return sess != nil && sess.room == room.Name && sess.roomSID == room.SID && sess.join == p.Join &&
		!s.departedLocked(p.SID) && (sess.companion == nil || sess.companion.p.SID == p.SID || newer(p, sess.companion.p))
}

// streaming combines primary and companion screen video tracks.
func (sess *session) streaming() bool {
	return len(sess.screens) > 0 || (sess.companion != nil && len(sess.companion.screens) > 0)
}

// syncStreamLocked announces only changes to the owner's aggregated stream flag.
func (s *Service) syncStreamLocked(ctx context.Context, sess *session) {
	if streaming := sess.streaming(); streaming != sess.state.SelfStream {
		sess.state.SelfStream = streaming
		s.touchLocked(sess.state.UserID)
		s.announceLocked(ctx, sess.state, false)
	}
}

// removeScreenLocked revokes a companion even when the primary already left.
func (s *Service) removeScreenLocked(ctx context.Context, sess *session) {
	if sess.companion == nil {
		return
	}
	s.removeLocked(sess.room, sess.state.UserID.String()+".screen", sess.companion.p.SID)
	sess.companion = nil
	s.touchLocked(sess.state.UserID)
	s.syncStreamLocked(ctx, sess)
}
