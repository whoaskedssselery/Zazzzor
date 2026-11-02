package voice

import (
	"context"
	"errors"
	"fmt"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// ModerationFields are server flags to change; nil preserves a flag.
type ModerationFields struct {
	Mute *bool
	Deaf *bool
	// Move: nil preserves the participant's channel.
	Move *MoveRequest
}

// MoveRequest takes a participant out of voice (Disconnect) or asks them
// to move to the voice channel ChannelID.
type MoveRequest struct {
	Disconnect bool
	ChannelID  snowflake.ID
}

// serverFlags survive reconnects within a guild until explicitly lifted.
type serverFlags struct{ mute, deaf bool }

// lockUser orders media changes and token issuance for one user. Fixed
// stripes bound the lock table; cancellation releases waiters before an RPC
// finishes, and state reads never need a stripe.
func (s *Service) lockUser(ctx context.Context, userID snowflake.ID) (func(), error) {
	// Defence in depth: a free stripe would win the select below even for a
	// cancelled ctx, and every caller rechecks ctx after locking.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("voice: wait for participant operation: %w", err)
	}
	index := userID % snowflake.ID(len(s.operations))
	if index < 0 {
		index = -index
	}
	stripe := s.operations[index]
	select {
	case stripe <- struct{}{}:
		return func() { <-stripe }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("voice: wait for participant operation: %w", ctx.Err())
	}
}

// Moderate changes a participant's server flags after checking every
// requested permission. A missing, hidden or other-guild target is 404.
// Flags and public state change only after LiveKit applies the grants.
func (s *Service) Moderate(ctx context.Context, guildID, actorID, userID snowflake.ID, f ModerationFields) error {
	if !s.enabled {
		return ErrUnavailable
	}
	if f.Mute == nil && f.Deaf == nil && f.Move == nil {
		return invalid("mute", "set mute, deaf or channel_id")
	}
	unlock, lockErr := s.lockUser(ctx, userID)
	if lockErr != nil {
		return lockErr
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("voice: moderate participant: %w", err)
	}
	actor, err := s.members.Subject(ctx, guildID, actorID)
	if errors.Is(err, guilds.ErrNotMember) {
		return channels.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("voice: read moderator membership: %w", err)
	}
	s.mu.Lock()
	sess := s.sessions[userID]
	if sess == nil || sess.state.GuildID != guildID {
		s.mu.Unlock()
		return channels.ErrNotFound
	}
	channelID, room := sess.state.ChannelID, sess.room
	key := memberKey{guildID: guildID, userID: userID}
	flags := s.moderation[key]
	start := s.rev
	s.mu.Unlock()
	p, gid, err := s.channels.Permissions(ctx, actorID, channelID)
	if err != nil {
		return fmt.Errorf("voice: read moderator permissions: %w", err)
	}
	// Defence in depth: the captured session already belongs to this guild.
	if gid != guildID {
		return channels.ErrNotFound
	}
	if (f.Mute != nil && !p.Has(permissions.MuteMembers)) || (f.Deaf != nil && !p.Has(permissions.DeafenMembers)) || (f.Move != nil && !p.Has(permissions.MoveMembers)) {
		return ErrMissingPerm
	}
	if actorID != userID {
		target, subjectErr := s.members.Subject(ctx, guildID, userID)
		if errors.Is(subjectErr, guilds.ErrNotMember) {
			return channels.ErrNotFound
		}
		if subjectErr != nil {
			return fmt.Errorf("voice: read participant membership: %w", subjectErr)
		}
		if !permissions.CanActOn(actor, target) {
			return guilds.ErrHierarchy
		}
	}
	if f.Move != nil && !f.Move.Disconnect {
		if err = s.checkDestination(ctx, guildID, actorID, userID, f.Move.ChannelID); err != nil {
			return err
		}
	}
	if f.Mute != nil {
		flags.mute = *f.Mute
	}
	if f.Deaf != nil {
		flags.deaf = *f.Deaf
	}
	if f.Move != nil && f.Move.Disconnect {
		return s.disconnect(ctx, guildID, userID, sess, flags, start)
	}
	flagsRequested := f.Mute != nil || f.Deaf != nil
	if flagsRequested {
		p, gid, err = s.channels.Permissions(ctx, userID, channelID)
		if err != nil {
			return fmt.Errorf("voice: read participant permissions: %w", err)
		}
		if gid != guildID || !p.Has(permissions.Connect) {
			return channels.ErrNotFound
		}
		if err = s.applyPermissions(ctx, room, userID.String(), p, flags); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if flagsRequested {
		// A successful RPC is reported as applied even when cleanup raced it.
		// Cleanup must remain authoritative for memberships and deleted channels.
		if s.invalidatedLocked(guildID, userID, channelID, start) {
			return nil
		}
		s.commitModerationLocked(ctx, key, flags)
	}
	if s.sessions[userID] != sess {
		if flagsRequested {
			if current := s.sessions[userID]; current != nil && current.state.GuildID == guildID {
				s.queueRefreshLocked(guildID)
			}
		}
		// Applied flags stay committed, but a requested move did not start.
		if f.Move == nil {
			return nil
		}
		return channels.ErrNotFound
	}
	if f.Move == nil {
		return nil
	}
	// A destination deleted since checkDestination read it starts no move;
	// flags already applied by LiveKit stay committed.
	if s.invalidatedLocked(guildID, userID, f.Move.ChannelID, start) {
		return channels.ErrNotFound
	}
	s.requestMoveLocked(ctx, userID, f.Move.ChannelID, sess)
	return nil
}

// commitModerationLocked stores applied flags for the membership. A replaced
// session keeps its call and receives the overlay through the refresh worker.
func (s *Service) commitModerationLocked(ctx context.Context, key memberKey, flags serverFlags) {
	s.storeModerationLocked(key, flags)
	// Even identical flags invalidate an older reconcile permission snapshot.
	s.touchLocked(key.userID)
	if current := s.sessions[key.userID]; current != nil && current.state.GuildID == key.guildID {
		if current.state.Mute != flags.mute || current.state.Deaf != flags.deaf {
			current.state.Mute, current.state.Deaf = flags.mute, flags.deaf
			s.announceLocked(ctx, current.state, false)
		}
	}
}

// storeModerationLocked retains only active server restrictions.
func (s *Service) storeModerationLocked(key memberKey, flags serverFlags) {
	if flags == (serverFlags{}) {
		delete(s.moderation, key)
	} else {
		s.moderation[key] = flags
	}
}

// applyPermissions replaces remote grants under a caller-held user lock.
// It never holds the state mutex across the bounded LiveKit RPC.
func (s *Service) applyPermissions(ctx context.Context, room, identity string, p permissions.Permission, flags serverFlags) error {
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	if err := s.rooms.UpdateParticipant(rpcCtx, room, identity, mediaPermissions(p, flags.mute, flags.deaf)); err != nil {
		return fmt.Errorf("%w: voice: apply participant permissions: %w", ErrUnavailable, err)
	}
	return nil
}

// enforceFound applies current moderation before a reconcile snapshot may
// become public state. The caller rechecks the snapshot after the RPC.
func (s *Service) enforceFound(ctx context.Context, userID snowflake.ID, f found, start uint64) (uint64, error) {
	unlock, lockErr := s.lockUser(ctx, userID)
	if lockErr != nil {
		return 0, lockErr
	}
	defer unlock()
	s.mu.Lock()
	if s.revs[userID] > start || !s.admissionCurrentLocked(ctx, f.guildID, userID, f.channelID, start, f.room, f.p) {
		s.mu.Unlock()
		return 0, nil
	}
	flags := s.moderation[memberKey{guildID: f.guildID, userID: userID}]
	s.mu.Unlock()
	if err := s.applyPermissions(ctx, f.room.Name, f.p.Identity, f.permissions, flags); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := s.admissionCurrentLocked(ctx, f.guildID, userID, f.channelID, start, f.room, f.p)
	unchanged := s.revs[userID] <= start
	// Record this successful RPC even for an existing SID, so another
	// reconcile cannot overwrite it with an older permissions snapshot.
	s.touchLocked(userID)
	// Defence in depth: Reconcile also rejects changed revisions at commit.
	if !allowed || !unchanged {
		return 0, nil
	}
	return s.revs[userID], nil
}
