package voice

import (
	"context"
	"maps"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// pruneRemovalsLocked bounds tombstones to the token/retry horizon. HTTP
// admission reads are bounded by request deadlines well below this window.
func (s *Service) pruneRemovalsLocked() {
	now := s.now()
	for sid, at := range s.departed {
		if now.Sub(at) > seenTTL {
			delete(s.departed, sid)
		}
	}
	for key, change := range s.membersRemoved {
		if now.Sub(change.at) > seenTTL {
			delete(s.membersRemoved, key)
		}
	}
	for key, change := range s.channelsRemoved {
		if now.Sub(change.at) > seenTTL {
			delete(s.channelsRemoved, key)
		}
	}
}

// departedLocked reports whether LiveKit already reported this SID gone.
func (s *Service) departedLocked(sid string) bool {
	s.pruneRemovalsLocked()
	_, ok := s.departed[sid]
	return ok
}

// invalidatedLocked rejects a permission snapshot older than a removal.
func (s *Service) invalidatedLocked(guildID, userID, channelID snowflake.ID, start uint64) bool {
	s.pruneRemovalsLocked()
	return s.membersRemoved[memberKey{guildID: guildID, userID: userID}].revision > start ||
		s.channelsRemoved[channelID].revision > start
}

// invalidationLocked records the current revision at the service's clock.
func (s *Service) invalidationLocked() invalidation {
	return invalidation{revision: s.rev, at: s.now()}
}

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
	if sess.state.GuildID == 0 {
		s.joinedCallLocked(ctx, sess.state.ChannelID, sess.state.UserID)
	}
}

// dropLocked clears userID's state, if any, and announces that they left.
// With remove, their participant is also removed from LiveKit.
func (s *Service) dropLocked(ctx context.Context, userID snowflake.ID, remove bool) {
	sess, ok := s.sessions[userID]
	if !ok {
		return
	}
	s.removeScreenLocked(ctx, sess)
	delete(s.sessions, userID)
	s.touchLocked(userID)
	s.announceLocked(ctx, sess.state, true)
	if sess.state.GuildID == 0 {
		s.leftCallLocked(ctx, sess.state.ChannelID)
	}
	if remove {
		delete(s.moves, userID)
		s.removeLocked(sess.room, userID.String(), sess.sid)
	}
}

// removeLocked queues the removal of a participant from LiveKit; Run
// carries it out. Coalesced jobs retain every known rejected SID.
func (s *Service) removeLocked(room, identity, sid string) {
	key := removalKey{room: room, identity: identity}
	targets := map[string]bool{sid: true}
	job := &removal{sid: sid, targets: targets, due: s.now()}
	if previous := s.removals[key]; previous != nil {
		if previous.matchesSID(sid) {
			return
		}
		maps.Copy(targets, previous.targets)
		targets[previous.sid] = true
		job.attempts, job.due = previous.attempts, previous.due
	}
	s.removals[key] = job
	select {
	case s.wake <- struct{}{}:
	default: // already woken
	}
}

// cancelRemovalLocked cancels only the SID that successfully reentered.
// Replacing the job preserves immutable target sets for an in-flight RPC.
func (s *Service) cancelRemovalLocked(key removalKey, sid string) {
	job := s.removals[key]
	if job == nil || !job.matchesSID(sid) {
		return
	}
	targets := maps.Clone(job.targets)
	if targets == nil {
		targets = map[string]bool{job.sid: true}
	}
	delete(targets, sid)
	if len(targets) == 0 {
		delete(s.removals, key)
		return
	}
	copyJob := *job
	copyJob.targets = targets
	if copyJob.sid == sid {
		for remaining := range targets {
			copyJob.sid = remaining
			break
		}
	}
	s.removals[key] = &copyJob
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
