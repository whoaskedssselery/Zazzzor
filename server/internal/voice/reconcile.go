package voice

import (
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Run does the background work until ctx ends: it rebuilds the states from
// LiveKit at once and then every ReconcileInterval, and carries out the
// queued removals of participants. It returns at once when voice is off.
func (s *Service) Run(ctx context.Context) {
	if !s.enabled {
		return
	}
	var wg sync.WaitGroup
	wg.Go(func() { s.runRemovals(ctx) })
	defer wg.Wait()

	ticker := time.NewTicker(ReconcileInterval)
	defer ticker.Stop()
	for {
		if err := s.Reconcile(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("voice: reconcile with LiveKit", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// found is an admitted participant of a managed room.
type found struct {
	guildID   snowflake.ID
	channelID snowflake.ID
	room      Room
	p         Participant
}

// Reconcile makes the states match what LiveKit reports: admitted
// participants of the latest join become states, participants who may not
// be there are removed, and states without a participant go. A room that
// could not be listed keeps its states; a state that changed while LiveKit
// was asked is left as it is; rooms that are not voice channels are left
// alone. A user in two rooms keeps the one joined last.
func (s *Service) Reconcile(ctx context.Context) error {
	s.mu.Lock()
	s.pruneRemovalsLocked()
	start := s.rev
	s.mu.Unlock()

	listCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	rooms, err := s.rooms.ListRooms(listCtx)
	cancel()
	if err != nil {
		return err
	}
	var (
		kept      = map[snowflake.ID]found{}
		turnedOut []found // admitted, but not the room the user is kept in
		refused   []found // not admitted
		unknown   = map[snowflake.ID]bool{}
	)
	for _, room := range rooms {
		channelID, ok := roomChannel(room.Name)
		if !ok {
			continue
		}
		listCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		ps, err := s.rooms.ListParticipants(listCtx, room.Name)
		cancel()
		if err != nil {
			s.log.Warn("voice: list participants", "room", room.Name, "err", err.Error())
			unknown[channelID] = true
			continue
		}
		for _, p := range ps {
			guildID, ok, err := s.admitted(ctx, channelID, p)
			if err != nil {
				return fmt.Errorf("voice: reconcile room %s: %w", room.Name, err)
			}
			f := found{guildID: guildID, channelID: channelID, room: room, p: p}
			if !ok {
				refused = append(refused, f)
				continue
			}
			userID, _ := identityUser(p.Identity)
			if prev, dup := kept[userID]; dup {
				if newer(f.p, prev.p) {
					kept[userID], f = f, prev
				}
				turnedOut = append(turnedOut, f)
				continue
			}
			kept[userID] = f
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := func(userID snowflake.ID) bool { return s.revs[userID] > start }
	for _, f := range refused {
		s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
	}
	for _, f := range turnedOut {
		if userID, _ := identityUser(f.p.Identity); !changed(userID) {
			s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
		}
	}
	for userID, f := range kept {
		switch {
		case s.departedLocked(f.p.SID):
		case s.invalidatedLocked(f.guildID, userID, f.channelID, start):
			s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
		case changed(userID):
		case !s.currentLocked(userID, f.channelID, f.p.Join):
			s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
		default:
			s.connectLocked(ctx, f.guildID, f.channelID, f.room, f.p)
			s.syncScreensLocked(ctx, userID, f.p)
		}
	}
	for userID, sess := range s.sessions {
		if f, ok := kept[userID]; (ok && f.p.SID == sess.sid) || changed(userID) || unknown[sess.state.ChannelID] {
			continue
		}
		s.dropLocked(ctx, userID, false)
	}
	return nil
}

// newer reports whether a joined after b; equal times fall back to the
// SID, so the choice does not depend on the order LiveKit lists rooms in.
func newer(a, b Participant) bool {
	if c := a.JoinedAt.Compare(b.JoinedAt); c != 0 {
		return c > 0
	}
	return cmp.Less(b.SID, a.SID)
}

// syncScreensLocked sets the screen shares of userID's state, which is of
// participant p, to what LiveKit listed, announcing a change of
// self_stream.
func (s *Service) syncScreensLocked(ctx context.Context, userID snowflake.ID, p Participant) {
	sess := s.sessions[userID]
	if sess == nil || sess.sid != p.SID {
		return
	}
	sess.screens = map[string]bool{}
	for _, sid := range p.Screens {
		sess.screens[sid] = true
	}
	if streaming := len(p.Screens) > 0; streaming != sess.state.SelfStream {
		sess.state.SelfStream = streaming
		s.touchLocked(userID)
		s.announceLocked(ctx, sess.state, false)
	}
}

// runRemovals carries out the queued removals until ctx ends. A failed one
// is tried again later, up to removeAttempts in all; the last failure is
// logged.
func (s *Service) runRemovals(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		next, ok := s.removeDue(ctx)
		if ctx.Err() != nil {
			return
		}
		if ok {
			timer.Reset(time.Until(next))
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-timer.C:
		}
	}
}

// removeDue carries out every removal that is due and returns when the
// next one is (false: none is queued).
func (s *Service) removeDue(ctx context.Context) (time.Time, bool) {
	for {
		s.mu.Lock()
		var (
			key  removalKey
			job  *removal
			next time.Time
		)
		now := s.now()
		for k, r := range s.removals {
			if !r.due.After(now) {
				key, job = k, r
				break
			}
			if next.IsZero() || r.due.Before(next) {
				next = r.due
			}
		}
		if job == nil {
			s.mu.Unlock()
			return next, !next.IsZero()
		}
		if s.rejoinedLocked(key, job) {
			delete(s.removals, key)
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()

		rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		err := s.removeParticipant(rpcCtx, key, job)
		cancel()
		if ctx.Err() != nil {
			return time.Time{}, false
		}

		s.mu.Lock()
		if s.removals[key] == job {
			job.attempts++
			switch {
			case err == nil:
				delete(s.removals, key)
			case job.attempts >= removeAttempts:
				delete(s.removals, key)
				s.log.Warn("voice: could not remove a participant from LiveKit", "room", key.room,
					"identity", key.identity, "participant", job.sid, "attempts", job.attempts, "err", err.Error())
			default:
				job.due = s.now().Add(min(removeBackoff<<(job.attempts-1), maxRemoveBackoff))
			}
		}
		s.mu.Unlock()
	}
}

// removeParticipant checks the remote SID too: its join webhook may not
// have arrived yet. LiveKit's identity-based API cannot make the check and
// removal atomic, so this narrows rather than eliminates that RPC race.
func (s *Service) removeParticipant(ctx context.Context, key removalKey, job *removal) error {
	if job.sid != "" {
		ps, err := s.rooms.ListParticipants(ctx, key.room)
		if err != nil {
			return fmt.Errorf("voice: inspect removal target: %w", err)
		}
		for _, p := range ps {
			if p.Identity == key.identity && p.SID != job.sid {
				return nil
			}
		}
	}
	return s.rooms.RemoveParticipant(ctx, key.room, key.identity)
}

// rejoinedLocked reports whether the user a removal is meant for is now in
// that room as another participant: removing by identity would then
// disconnect their new session.
func (s *Service) rejoinedLocked(key removalKey, job *removal) bool {
	userID, ok := identityUser(key.identity)
	if !ok || job.sid == "" {
		return false
	}
	sess := s.sessions[userID]
	return sess != nil && sess.room == key.room && sess.sid != job.sid
}
