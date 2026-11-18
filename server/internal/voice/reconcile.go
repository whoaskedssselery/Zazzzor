package voice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
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
	wg.Go(func() { s.runRefreshes(ctx) })
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
	guildID     snowflake.ID
	channelID   snowflake.ID
	room        Room
	p           Participant
	permissions permissions.Permission
}

// Reconcile makes the states match what LiveKit reports: admitted
// participants of the latest join become states, participants who may not
// be there are removed, and states without a participant go. A room that
// could not be checked keeps its states; a state that changed while LiveKit
// was asked is left as it is; rooms that are not voice channels are left
// alone. A user in two rooms keeps the one joined last. Room failures are
// returned after healthy rooms are applied.
func (s *Service) Reconcile(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("voice: reconcile: %w", err)
	}
	s.mu.Lock()
	s.expireMovesLocked(ctx)
	s.pruneRemovalsLocked()
	start := s.rev
	permissionRevision := s.permissionRev
	s.mu.Unlock()

	listCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	rooms, err := s.rooms.ListRooms(listCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("voice: reconcile: list rooms: %w", err)
	}
	var (
		kept            = map[snowflake.ID]found{}
		sources         = map[snowflake.ID]found{}
		turnedOut       []found // admitted, but not the room the user is kept in
		refused         []found // not admitted
		screens         []found // resolved only after primary sessions
		unknown         = map[snowflake.ID]bool{}
		reconcileErrors []error
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
			reconcileErrors = append(reconcileErrors, fmt.Errorf("voice: reconcile: list participants in %s: %w", room.Name, err))
			unknown[channelID] = true
			continue
		}
		var candidates, roomRefused []found
		for _, p := range ps {
			if _, secondary := screenOwner(p.Identity); secondary {
				screens = append(screens, found{channelID: channelID, room: room, p: p})
				continue
			}
			guildID, mask, ok, err := s.admitted(ctx, channelID, p)
			if err != nil {
				unknown[channelID] = true
				reconcileErrors = append(reconcileErrors, fmt.Errorf("voice: reconcile room %s: %w", room.Name, err))
				break
			}
			f := found{guildID: guildID, channelID: channelID, room: room, p: p, permissions: mask}
			if !ok {
				roomRefused = append(roomRefused, f)
				continue
			}
			candidates = append(candidates, f)
		}
		if unknown[channelID] {
			continue
		}
		refused = append(refused, roomRefused...)
		for _, f := range candidates {
			p := f.p
			userID, _ := identityUser(p.Identity)
			s.mu.Lock()
			current := !s.departedLocked(p.SID) && !s.invalidatedLocked(f.guildID, userID, channelID, start) && s.currentParticipantLocked(userID, channelID, p)
			if move := s.moves[userID]; current && move != nil && p.SID == move.source.sid && channelID == move.source.state.ChannelID {
				sources[userID] = f
			}
			s.mu.Unlock()
			if !current {
				refused = append(refused, f)
				continue
			}
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
	checked, failed := map[snowflake.ID]uint64{}, map[snowflake.ID]bool{}
	for userID, f := range kept {
		revision, err := s.enforceFound(ctx, userID, f, start)
		if err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("voice: reconcile user %s permissions: %w", userID, err))
			// A failed destination has not completed the handoff. The observed
			// source still needs its own current permissions enforced.
			if source, ok := sources[userID]; ok && source.p.SID != f.p.SID {
				refused = append(refused, f)
				kept[userID] = source
				revision, err = s.enforceFound(ctx, userID, source, start)
				if err != nil {
					reconcileErrors = append(reconcileErrors, fmt.Errorf("voice: reconcile source permissions: %w", err))
				}
			}
			if err != nil {
				failed[userID] = true
				continue
			}
		}
		checked[userID] = revision
	}

	observed := map[snowflake.ID]*screenObservation{}
	var screenCandidates []found
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		changed := func(userID snowflake.ID) bool { return s.revs[userID] > max(start, checked[userID]) }
		for userID := range s.sessions {
			if !changed(userID) {
				observed[userID] = &screenObservation{}
			}
		}
		for _, f := range screens {
			userID, _ := screenOwner(f.p.Identity)
			if !changed(userID) {
				observed[userID] = &screenObservation{}
			}
		}
		for _, f := range refused {
			s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
		}
		for _, f := range turnedOut {
			if userID, _ := identityUser(f.p.Identity); !changed(userID) && kept[userID].p.SID != f.p.SID {
				s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
			}
		}
		for userID, f := range kept {
			switch {
			case s.departedLocked(f.p.SID):
			case s.invalidatedLocked(f.guildID, userID, f.channelID, start):
				s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
			case changed(userID):
			case failed[userID]:
				// A connected session keeps its call through a LiveKit blip; the
				// refresh worker retries its grants. A participant not yet admitted
				// has no known grants and is removed.
				if sess := s.sessions[userID]; sess != nil && sess.sid == f.p.SID {
					s.queueRefreshLocked(f.guildID)
					continue
				}
				s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
			case !s.currentParticipantLocked(userID, f.channelID, f.p):
				s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
			default:
				// Another room's duplicate cannot displace a session whose room
				// could not be checked, particularly after restart without intents.
				if current := s.sessions[userID]; current != nil && unknown[current.state.ChannelID] && current.state.ChannelID != f.channelID {
					continue
				}
				s.connectLocked(ctx, f.guildID, f.channelID, f.room, f.p)
				s.syncScreensLocked(ctx, userID, f.p)
				if s.permissionRev != permissionRevision {
					s.queueRefreshLocked(f.guildID)
				}
			}
		}
		for userID, sess := range s.sessions {
			if f, ok := kept[userID]; (ok && f.p.SID == sess.sid) || changed(userID) || unknown[sess.state.ChannelID] {
				continue
			}
			s.dropLocked(ctx, userID, false)
		}
		// Capture the revisions after our primary updates, but only for owners
		// unchanged by concurrent work since listing began.
		listed := map[removalKey]Participant{}
		for _, f := range screens {
			listed[removalKey{f.room.Name, f.p.Identity}] = f.p
		}
		for userID, observation := range observed {
			sess := s.sessions[userID]
			if sess != nil && !unknown[sess.state.ChannelID] && sess.companion != nil {
				p, exists := listed[removalKey{sess.room, userID.String() + ".screen"}]
				if !exists || p.SID != sess.companion.p.SID {
					s.removeScreenLocked(ctx, sess)
				}
			}
			observation.sess, observation.revision = sess, s.revs[userID]
		}
		// Reject all mismatched bindings before accepting any companion:
		// accepting one advances its owner's revision, which must not hide
		// another room's orphan behind the observation guard.
		for _, f := range screens {
			userID, _ := screenOwner(f.p.Identity)
			if observation := observed[userID]; observation != nil && !unknown[f.channelID] {
				if !s.screenCurrentLocked(observation.sess, f.room, f.p) {
					s.removeLocked(f.room.Name, f.p.Identity, f.p.SID)
					continue
				}
				screenCandidates = append(screenCandidates, f)
			}
		}
	}()
	for _, f := range screenCandidates {
		userID, _ := screenOwner(f.p.Identity)
		if err := s.joinedScreen(ctx, f.channelID, f.room, f.p, observed[userID]); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("voice: reconcile companion: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		reconcileErrors = append(reconcileErrors, fmt.Errorf("voice: reconcile: %w", err))
	}
	if len(reconcileErrors) == 0 {
		s.mu.Lock()
		s.lastReconcile = s.now()
		s.mu.Unlock()
	}
	return errors.Join(reconcileErrors...)
}

// newer reports whether a joined after b; equal times fall back to the
// SID, so the choice does not depend on the order LiveKit lists rooms in.
func newer(a, b Participant) bool {
	if c := a.JoinedAt.Compare(b.JoinedAt); c != 0 {
		return c > 0
	}
	return cmp.Less(b.SID, a.SID)
}

// syncScreensLocked sets the screen shares and cameras of userID's state,
// which is of participant p, to what LiveKit listed, announcing a change of
// self_stream or self_video once.
func (s *Service) syncScreensLocked(ctx context.Context, userID snowflake.ID, p Participant) {
	sess := s.sessions[userID]
	if sess == nil || sess.sid != p.SID {
		return
	}
	sess.screens = map[string]bool{}
	for _, sid := range p.Screens {
		sess.screens[sid] = true
	}
	sess.cameras = map[string]bool{}
	for _, sid := range p.Cameras {
		sess.cameras[sid] = true
	}
	streaming, video := sess.streaming(), len(p.Cameras) > 0
	if streaming != sess.state.SelfStream || video != sess.state.SelfVideo {
		sess.state.SelfStream, sess.state.SelfVideo = streaming, video
		s.touchLocked(userID)
		s.announceLocked(ctx, sess.state, false)
	}
}

// runRemovals carries out the queued removals until ctx ends. A failed one
// is retried with capped backoff until success; persistent failure is logged
// from removeAttempts failures on, and so is the recovery after it.
func (s *Service) runRemovals(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		next, ok := s.removeDue(ctx)
		if ctx.Err() != nil {
			return
		}
		if ok {
			timer.Reset(next.Sub(s.now()))
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
		next = s.expireMovesLocked(ctx)
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
		s.mu.Unlock()

		rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		err := s.removeParticipant(rpcCtx, key, job)
		cancel()
		if ctx.Err() != nil {
			return time.Time{}, false
		}

		s.mu.Lock()
		current := s.removals[key]
		warn, recovered := false, false
		attempts, sid := 0, ""
		if current == job && err == nil {
			delete(s.removals, key)
			recovered = job.attempts >= removeAttempts
			attempts, sid = job.attempts, job.sid
		} else if current != nil && err != nil {
			// A replacement retains the failed RPC's attempt and deadline too.
			current.attempts = max(current.attempts, job.attempts) + 1
			backoff := removeBackoff
			for attempt := 1; attempt < current.attempts && backoff < maxRemoveBackoff; attempt++ {
				backoff = min(backoff*2, maxRemoveBackoff)
			}
			current.due = s.now().Add(backoff)
			// Warn once the retries look like an outage, then again about
			// hourly at the backoff cap while it lasts.
			warn = current.attempts >= removeAttempts && (current.attempts-removeAttempts)%removeWarnEvery == 0
			attempts, sid = current.attempts, current.sid
		}
		s.mu.Unlock()
		if warn {
			s.log.Warn("voice: could not remove a participant from LiveKit; retrying", "room", key.room,
				"identity", key.identity, "participant", sid, "attempts", attempts, "err", err.Error())
		}
		if recovered {
			s.log.Info("voice: removed a participant from LiveKit after retries", "room", key.room,
				"identity", key.identity, "participant", sid, "attempts", attempts)
		}
	}
}

// removeParticipant checks the remote SID too: its join webhook may not
// have arrived yet. LiveKit's identity-based API cannot make the check and
// removal atomic, so this narrows rather than eliminates that RPC race.
func (s *Service) removeParticipant(ctx context.Context, key removalKey, job *removal) error {
	found := false
	if job.sid != "" {
		ps, err := s.rooms.ListParticipants(ctx, key.room)
		if err != nil {
			return fmt.Errorf("voice: inspect removal target: %w", err)
		}
		for _, p := range ps {
			if p.Identity == key.identity {
				found = true
				if !job.matchesSID(p.SID) {
					return nil
				}
			}
		}
	}
	// Admission may have recovered while ListParticipants was in flight.
	s.mu.Lock()
	current := s.removals[key] == job
	if !found && s.rejoinedLocked(key, job) {
		current = false
	}
	s.mu.Unlock()
	if !current {
		return nil
	}
	return s.rooms.RemoveParticipant(ctx, key.room, key.identity)
}

// rejoinedLocked reports whether the user a removal is meant for is now in
// that room as another participant: removing by identity would then
// disconnect their new session.
func (s *Service) rejoinedLocked(key removalKey, job *removal) bool {
	if userID, ok := screenOwner(key.identity); ok {
		sess := s.sessions[userID]
		return job.sid != "" && sess != nil && sess.room == key.room && sess.companion != nil && !job.matchesSID(sess.companion.p.SID)
	}
	userID, ok := identityUser(key.identity)
	if !ok || job.sid == "" {
		return false
	}
	sess := s.sessions[userID]
	return sess != nil && sess.room == key.room && !job.matchesSID(sess.sid)
}
