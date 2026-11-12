package voice

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// defaultRingFor is how long a call rings a recipient who neither joins nor declines.
const defaultRingFor = 60 * time.Second

// Calls are voice in a DM or a group DM. A call lives while someone is in
// it or it still rings someone; it lives in this process only, like the
// voice states, so a restart ends it.

// Call is the JSON form of a call (schema Call).
type Call struct {
	ChannelID snowflake.ID   `json:"channel_id"`
	Ringing   []snowflake.ID `json:"ringing"`
}

// CallDelete is the payload of CALL_DELETE.
type CallDelete struct {
	ChannelID snowflake.ID `json:"channel_id"`
}

// Private is what calls need from the channels module.
type Private interface {
	// PrepareCall checks that userID may call in private channel channelID
	// (a 1:1 on the terms of a message), reopens a closed 1:1 for the
	// other recipient, and returns every recipient.
	PrepareCall(ctx context.Context, channelID, userID snowflake.ID) ([]snowflake.ID, error)
}

var _ Private = (*channels.Service)(nil)

// call is a call's ringing list: each rung recipient and when it stops.
type call struct {
	ringing map[snowflake.ID]time.Time
	timer   *time.Timer
}

func (c *call) public(channelID snowflake.ID) Call {
	out := Call{ChannelID: channelID, Ringing: make([]snowflake.ID, 0, len(c.ringing))}
	for id := range c.ringing {
		out.Ringing = append(out.Ringing, id)
	}
	slices.SortFunc(out.Ringing, func(a, b snowflake.ID) int { return cmp.Compare(a, b) })
	return out
}

// ringCall starts the call of channelID when nobody is in it, ringing every
// recipient but userID and those already in it; on a call going on, it
// stops ringing userID, who is joining.
func (s *Service) ringCall(ctx context.Context, channelID, userID snowflake.ID, recipients []snowflake.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.calls[channelID]; c != nil {
		if _, ok := c.ringing[userID]; ok {
			delete(c.ringing, userID)
			s.publishCallLocked(ctx, events.CallUpdate, channelID, c.public(channelID))
		}
		return
	}
	inCall := s.inCallLocked(channelID)
	c := &call{ringing: map[snowflake.ID]time.Time{}}
	until := s.now().Add(s.ringFor)
	for _, id := range recipients {
		if id != userID && !slices.Contains(inCall, id) {
			c.ringing[id] = until
		}
	}
	s.calls[channelID] = c
	c.timer = time.AfterFunc(s.ringFor, func() { s.stopRinging(context.WithoutCancel(ctx), channelID) })
	s.publishCallLocked(ctx, events.CallCreate, channelID, c.public(channelID))
}

// stopRinging takes the recipients whose ringing ran out off the call.
func (s *Service) stopRinging(ctx context.Context, channelID snowflake.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.calls[channelID]
	if c == nil {
		return
	}
	now := s.now()
	changed := false
	for id, until := range c.ringing {
		if !until.After(now) {
			delete(c.ringing, id)
			changed = true
		}
	}
	if s.endCallIfIdleLocked(ctx, channelID) || !changed {
		return
	}
	s.publishCallLocked(ctx, events.CallUpdate, channelID, c.public(channelID))
}

// DeclineCall stops the call of private channel channelID ringing userID.
func (s *Service) DeclineCall(ctx context.Context, channelID snowflake.ID, user auth.User) error {
	if !s.enabled {
		return ErrUnavailable
	}
	if _, _, err := s.channels.Permissions(ctx, user.ID, channelID); err != nil {
		return err
	}
	c, err := s.channels.Lookup(ctx, channelID)
	if err != nil {
		return err
	}
	if !c.Type.Private() {
		return ErrNotVoiceChannel
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	call := s.calls[channelID]
	if call == nil {
		return nil
	}
	if _, ok := call.ringing[user.ID]; !ok {
		return nil
	}
	delete(call.ringing, user.ID)
	if !s.endCallIfIdleLocked(ctx, channelID) {
		s.publishCallLocked(ctx, events.CallUpdate, channelID, call.public(channelID))
	}
	return nil
}

// joinedCallLocked stops ringing a user who is now in the call; a call the
// process does not know (it restarted) starts again, ringing nobody.
func (s *Service) joinedCallLocked(ctx context.Context, channelID, userID snowflake.ID) {
	c := s.calls[channelID]
	if c == nil {
		s.calls[channelID] = &call{ringing: map[snowflake.ID]time.Time{}}
		s.publishCallLocked(ctx, events.CallCreate, channelID, Call{ChannelID: channelID, Ringing: []snowflake.ID{}})
		return
	}
	if _, ok := c.ringing[userID]; ok {
		delete(c.ringing, userID)
		s.publishCallLocked(ctx, events.CallUpdate, channelID, c.public(channelID))
	}
}

// endCallIfIdleLocked ends the call of channelID when nobody is in it and
// it rings nobody; it reports whether it did.
func (s *Service) endCallIfIdleLocked(ctx context.Context, channelID snowflake.ID) bool {
	c := s.calls[channelID]
	if c == nil || len(c.ringing) > 0 || len(s.inCallLocked(channelID)) > 0 {
		return false
	}
	s.endCallLocked(ctx, channelID)
	return true
}

// endCallLocked forgets the call of channelID and says so.
func (s *Service) endCallLocked(ctx context.Context, channelID snowflake.ID) {
	c := s.calls[channelID]
	if c == nil {
		return
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	delete(s.calls, channelID)
	s.publishCallLocked(ctx, events.CallDelete, channelID, CallDelete{ChannelID: channelID})
}

// leftCallLocked ends the call when its last participant left. A call
// that still rings someone ends too: whoever started it is gone.
func (s *Service) leftCallLocked(ctx context.Context, channelID snowflake.ID) {
	if s.calls[channelID] != nil && len(s.inCallLocked(channelID)) == 0 {
		s.endCallLocked(ctx, channelID)
	}
}

// inCallLocked lists the users in the call of channelID.
func (s *Service) inCallLocked(channelID snowflake.ID) []snowflake.ID {
	var out []snowflake.ID
	for userID, sess := range s.sessions {
		if sess.state.GuildID == 0 && sess.state.ChannelID == channelID {
			out = append(out, userID)
		}
	}
	return out
}

func (s *Service) publishCallLocked(ctx context.Context, typ string, channelID snowflake.ID, payload any) {
	if s.pub == nil {
		return
	}
	s.pub.Publish(ctx, events.Event{Type: typ,
		Audience: events.Audience{Kind: events.AudienceChannel, ChannelID: channelID}, Payload: payload})
}

// PrivateCalls returns the calls going on in channelIDs and who is in them, for READY.
func (s *Service) PrivateCalls(channelIDs []snowflake.ID) ([]Call, []State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	calls, states := []Call{}, []State{}
	for _, id := range channelIDs {
		if c := s.calls[id]; c != nil {
			calls = append(calls, c.public(id))
		}
	}
	for _, sess := range s.sessions {
		if sess.state.GuildID == 0 && slices.Contains(channelIDs, sess.state.ChannelID) {
			states = append(states, sess.state)
		}
	}
	slices.SortFunc(calls, func(a, b Call) int { return cmp.Compare(a.ChannelID, b.ChannelID) })
	slices.SortFunc(states, func(a, b State) int { return cmp.Compare(a.UserID, b.UserID) })
	return calls, states
}

// RecheckPrivate takes userID out of the call of channelID when they are no
// longer a recipient (they left the group or were removed). A closed 1:1
// keeps its recipient, so its call goes on.
func (s *Service) RecheckPrivate(ctx context.Context, channelID, userID snowflake.ID) {
	_, _, err := s.channels.Permissions(ctx, userID, channelID)
	if !errors.Is(err, channels.ErrNotFound) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.calls[channelID]; c != nil {
		if _, ok := c.ringing[userID]; ok {
			delete(c.ringing, userID)
			if !s.endCallIfIdleLocked(ctx, channelID) {
				s.publishCallLocked(ctx, events.CallUpdate, channelID, c.public(channelID))
			}
		}
	}
	if s.intents[userID].channelID == channelID {
		s.intents[userID] = intent{}
	}
	if sess := s.sessions[userID]; sess != nil && sess.state.GuildID == 0 && sess.state.ChannelID == channelID {
		s.dropLocked(ctx, userID, true)
	}
}
