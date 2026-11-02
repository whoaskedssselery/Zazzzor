package voice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// permissionRefresh is one guild generation; a newer sync replaces its pointer.
type permissionRefresh struct {
	due     time.Time
	backoff time.Duration
}

// RefreshGuild queues current voice permissions after a committed guild change.
// It performs no I/O and coalesces requests for a guild with active sessions.
func (s *Service) RefreshGuild(guildID snowflake.ID) {
	if !s.enabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permissionRev++
	for _, sess := range s.sessions {
		if sess.state.GuildID == guildID {
			s.queueRefreshLocked(guildID)
			return
		}
	}
}

// queueRefreshLocked also covers admission that straddled an earlier sync.
func (s *Service) queueRefreshLocked(guildID snowflake.ID) {
	s.refreshes[guildID] = &permissionRefresh{due: s.now()}
	select {
	case s.refreshWake <- struct{}{}:
	default:
	}
}

// runRefreshes keeps slow permission reads independent of move deadlines.
func (s *Service) runRefreshes(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		next, pending := s.refreshDue(ctx)
		if ctx.Err() != nil {
			return
		}
		if pending {
			timer.Reset(next.Sub(s.now()))
		}
		select {
		case <-ctx.Done():
			return
		case <-s.refreshWake:
		case <-timer.C:
		}
	}
}

// refreshDue processes due guilds and retains failures with capped backoff.
// A sync arriving during the work remains queued as a different generation.
func (s *Service) refreshDue(ctx context.Context) (time.Time, bool) {
	for ctx.Err() == nil {
		s.mu.Lock()
		var guildID snowflake.ID
		var job *permissionRefresh
		var next time.Time
		for id, candidate := range s.refreshes {
			if !candidate.due.After(s.now()) {
				guildID, job = id, candidate
				break
			}
			if next.IsZero() || candidate.due.Before(next) {
				next = candidate.due
			}
		}
		s.mu.Unlock()
		if job == nil {
			return next, !next.IsZero()
		}
		err := s.refreshGuild(ctx, guildID, job)
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		if s.refreshes[guildID] == job {
			if err == nil {
				delete(s.refreshes, guildID)
			} else {
				job.backoff = min(max(job.backoff*2, removeBackoff), maxRemoveBackoff)
				job.due = s.now().Add(job.backoff)
				s.log.Warn("voice: refresh permissions; retrying", "guild", guildID,
					"retry_in", job.backoff, "err", err.Error())
			}
		}
		s.mu.Unlock()
	}
	return time.Time{}, false
}

// refreshGuild isolates failures so a single user cannot block the others.
func (s *Service) refreshGuild(ctx context.Context, guildID snowflake.ID, job *permissionRefresh) error {
	s.mu.Lock()
	participants := map[snowflake.ID]*session{}
	for userID, sess := range s.sessions {
		if sess.state.GuildID == guildID {
			participants[userID] = sess
		}
	}
	s.mu.Unlock()
	var failures []error
	for userID, sess := range participants {
		if err := s.refreshParticipant(ctx, guildID, userID, job, sess); err != nil {
			failures = append(failures, fmt.Errorf("voice: refresh user %s: %w", userID, err))
		}
	}
	return errors.Join(failures...)
}

// refreshParticipant orders permission RPCs with admission and moderation.
// Captured session and generation checks prevent stale local completion.
func (s *Service) refreshParticipant(ctx context.Context, guildID, userID snowflake.ID, job *permissionRefresh, captured *session) error {
	unlock, lockErr := s.lockUser(ctx, userID)
	if lockErr != nil {
		return lockErr
	}
	defer unlock()
	s.mu.Lock()
	if s.sessions[userID] != captured || s.refreshes[guildID] != job {
		s.mu.Unlock()
		return nil
	}
	channelID, room := captured.state.ChannelID, captured.room
	flags := s.moderation[memberKey{guildID: guildID, userID: userID}]
	move := s.moves[userID]
	s.mu.Unlock()
	workCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	if move != nil && move.source == captured {
		if err := s.refreshMove(workCtx, guildID, userID, move); err != nil {
			return err
		}
	}
	p, gid, err := s.channels.Permissions(workCtx, userID, channelID)
	lost := errors.Is(err, channels.ErrNotFound) || (err == nil && (gid != guildID || !p.Has(permissions.ViewChannel|permissions.Connect)))
	if err != nil && !lost {
		return fmt.Errorf("voice: read active participant permissions: %w", err)
	}
	s.mu.Lock()
	current := s.sessions[userID] == captured && s.refreshes[guildID] == job
	s.mu.Unlock()
	if !current {
		return nil
	}
	if lost {
		if err = s.rooms.RemoveParticipant(workCtx, room, userID.String()); err != nil {
			return fmt.Errorf("voice: remove participant after permission loss: %w", err)
		}
	} else if err = s.applyPermissions(workCtx, room, userID.String(), p, flags); err != nil {
		return fmt.Errorf("voice: enforce active participant permissions: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[userID] != captured {
		return nil
	}
	if lost {
		// Successful removal is authoritative even if a newer sync arrived.
		s.intents[userID] = intent{}
		delete(s.moves, userID)
		s.dropLocked(ctx, userID, false)
		return nil
	}
	if s.refreshes[guildID] != job {
		return nil
	}
	s.touchLocked(userID)
	if !p.Has(permissions.Stream) && captured.state.SelfStream {
		captured.screens = map[string]bool{}
		captured.state.SelfStream = false
		s.announceLocked(ctx, captured.state, false)
	}
	return nil
}

// refreshMove cancels userID's pending move once they may no longer view or
// CONNECT to its destination; the handoff could only end in a removal.
func (s *Service) refreshMove(ctx context.Context, guildID, userID snowflake.ID, move *pendingMove) error {
	p, gid, err := s.channels.Permissions(ctx, userID, move.channelID)
	if err != nil && !errors.Is(err, channels.ErrNotFound) {
		return fmt.Errorf("voice: read move destination permissions: %w", err)
	}
	if err == nil && gid == guildID && p.Has(permissions.ViewChannel|permissions.Connect) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.moves[userID] == move {
		s.cancelMoveLocked(ctx, userID)
	}
	return nil
}
