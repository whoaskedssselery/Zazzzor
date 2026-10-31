package voice

import (
	"context"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// The helpers below require s.mu. Events are published under it, so they
// go out in the order the states changed; Publish never blocks.

// touchLocked records a change of userID's state.
func (s *Service) touchLocked(userID snowflake.ID) {
	s.rev++
	s.revs[userID] = s.rev
}

// announceLocked publishes VOICE_STATE_UPDATE to the viewers of the state's
// channel.
func (s *Service) announceLocked(ctx context.Context, st State, left bool) {
	if s.pub == nil {
		return
	}
	s.pub.Publish(ctx, events.Event{Type: events.VoiceStateUpdate,
		Audience: events.Audience{Kind: events.AudienceChannel, GuildID: st.GuildID, ChannelID: st.ChannelID},
		Payload:  UpdatePayload{PublicState: st.Public(), Left: left}})
}

// setLocked makes sess the user's state and announces it.
func (s *Service) setLocked(ctx context.Context, sess *session) {
	s.sessions[sess.state.UserID] = sess
	s.touchLocked(sess.state.UserID)
	s.announceLocked(ctx, sess.state, false)
}

// dropLocked clears userID's state, if any, and announces that they left.
// With remove, their participant is also removed from LiveKit.
func (s *Service) dropLocked(ctx context.Context, userID snowflake.ID, remove bool) {
	sess, ok := s.sessions[userID]
	if !ok {
		return
	}
	delete(s.sessions, userID)
	s.touchLocked(userID)
	s.announceLocked(ctx, sess.state, true)
	if remove {
		s.removeLocked(sess.room, userID.String(), sess.sid)
	}
}

// removeLocked queues the removal of a participant from LiveKit; Run
// carries it out. A newer removal of the same room and identity replaces
// an older one.
func (s *Service) removeLocked(room, identity, sid string) {
	s.removals[removalKey{room: room, identity: identity}] = &removal{sid: sid, due: s.now()}
	select {
	case s.wake <- struct{}{}:
	default: // already woken
	}
}

// roomChannel returns the channel a room is named after: the room name is
// a channel ID exactly as ID.String() writes it. Other rooms are not ours.
func roomChannel(name string) (snowflake.ID, bool) {
	id, err := snowflake.Parse(name)
	if err != nil || id <= 0 || id.String() != name {
		return 0, false
	}
	return id, true
}

// identityUser returns the user a participant identity names, like
// roomChannel.
func identityUser(identity string) (snowflake.ID, bool) { return roomChannel(identity) }
