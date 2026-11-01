package voice

import (
	"context"
	"fmt"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Join is the answer to joining: a LiveKit token and where to use it.
type Join struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

// Join issues user a token for the voice channel channelID; they need
// VIEW_CHANNEL and CONNECT there, and its grants follow SPEAK and STREAM.
// origin is the request's Origin, already allowed; the signaling URL is
// built from it. A user in another voice channel is removed from it first;
// if LiveKit cannot remove them, the join is ErrUnavailable. The state
// itself appears when LiveKit reports the connection, and only a token of
// the latest join may connect.
func (s *Service) Join(ctx context.Context, channelID snowflake.ID, user auth.User, origin string) (Join, error) {
	if !s.enabled {
		return Join{}, ErrUnavailable
	}
	defer s.lockUser(user.ID)()
	if err := ctx.Err(); err != nil {
		return Join{}, fmt.Errorf("voice: issue join token: %w", err)
	}
	p, _, err := s.channels.Permissions(ctx, user.ID, channelID)
	if err != nil {
		return Join{}, err
	}
	c, err := s.channels.Lookup(ctx, channelID)
	if err != nil {
		return Join{}, err
	}
	if c.Type != channels.TypeVoice {
		return Join{}, ErrNotVoiceChannel
	}
	if !p.Has(permissions.Connect) {
		return Join{}, ErrMissingPerm
	}
	signaling, err := signalingURL(origin)
	if err != nil {
		return Join{}, err
	}
	if err = s.leaveOther(ctx, user.ID, channelID); err != nil {
		return Join{}, err
	}
	join := newJoin()
	s.mu.Lock()
	s.intents[user.ID] = intent{guildID: c.GuildID, channelID: channelID, join: join}
	flags := s.moderation[memberKey{guildID: c.GuildID, userID: user.ID}]
	s.mu.Unlock()
	token, err := s.token(user, channelID, p, join, flags)
	if err != nil {
		return Join{}, err
	}
	return Join{Token: token, URL: signaling}, nil
}

// leaveOther removes userID from LiveKit and clears their state when they
// are in a voice channel other than channelID. Joining the same channel
// again needs nothing: LiveKit replaces a participant whose identity
// connects again.
func (s *Service) leaveOther(ctx context.Context, userID, channelID snowflake.ID) error {
	s.mu.Lock()
	sess := s.sessions[userID]
	var room, sid string
	if sess != nil && sess.state.ChannelID != channelID {
		room, sid = sess.room, sess.sid
	}
	s.mu.Unlock()
	if sid == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	if err := s.rooms.RemoveParticipant(ctx, room, userID.String()); err != nil {
		return fmt.Errorf("%w: leave the previous channel: %w", ErrUnavailable, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.sessions[userID]; cur != nil && cur.sid == sid {
		s.dropLocked(ctx, userID, false)
	}
	return nil
}

// SelfFields are the self flags to change; nil leaves one as it is.
type SelfFields struct {
	SelfMute *bool
	SelfDeaf *bool
}

// UpdateSelf changes userID's self flags and announces a change; the same
// values again announce nothing. Without a state it is ErrNotInVoice.
func (s *Service) UpdateSelf(ctx context.Context, userID snowflake.ID, f SelfFields) (State, error) {
	if !s.enabled {
		return State{}, ErrUnavailable
	}
	if f.SelfMute == nil && f.SelfDeaf == nil {
		return State{}, invalid("self_mute", "set self_mute, self_deaf or both")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[userID]
	if sess == nil {
		return State{}, ErrNotInVoice
	}
	st := sess.state
	if f.SelfMute != nil {
		st.SelfMute = *f.SelfMute
	}
	if f.SelfDeaf != nil {
		st.SelfDeaf = *f.SelfDeaf
	}
	if st != sess.state {
		sess.state = st
		s.touchLocked(userID)
		s.announceLocked(ctx, st, false)
	}
	return st, nil
}

// Leave clears userID's state, announcing that they left, and removes them
// from LiveKit in the background; a token issued before can no longer
// connect. Not being in voice is fine.
func (s *Service) Leave(ctx context.Context, userID snowflake.ID) error {
	if !s.enabled {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intents[userID] = intent{}
	s.dropLocked(ctx, userID, true)
	return nil
}
