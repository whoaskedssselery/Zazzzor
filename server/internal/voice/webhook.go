package voice

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// LiveKit webhook events the service acts on.
const (
	eventJoined   = "participant_joined"
	eventLeft     = "participant_left"
	eventAborted  = "participant_connection_aborted"
	eventPublish  = "track_published"
	eventUnpub    = "track_unpublished"
	eventFinished = "room_finished"
)

// MountWebhook registers POST WebhookPath on r, without a session: LiveKit
// signs every webhook instead. The route needs no Origin either; the router
// exempts exactly this method and path.
func MountWebhook(r chi.Router, s *Service) {
	r.Post(WebhookPath, httpx.Wrap(s.handleWebhook))
}

// handleWebhook verifies a webhook before anything in its body is used,
// then applies it.
func (s *Service) handleWebhook(w http.ResponseWriter, r *http.Request) error {
	if !s.enabled {
		return fail(ErrUnavailable)
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBytes))
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return httpx.BadRequest("body_too_large", fmt.Sprintf("request body exceeds %d bytes", MaxWebhookBytes))
	}
	if err != nil {
		return fmt.Errorf("voice: read webhook: %w", err)
	}
	if err = s.verify(r.Header.Get("Authorization"), body); err != nil {
		httpx.LoggerFrom(r.Context()).WarnContext(r.Context(), "rejected LiveKit webhook", "err", err.Error())
		return fail(ErrInvalidSignature)
	}
	var ev livekit.WebhookEvent
	if err = (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, &ev); err != nil {
		return httpx.BadRequest("invalid_body", "the body is not a LiveKit webhook event")
	}
	if err = s.apply(r.Context(), &ev); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// verify checks LiveKit's signature: authorization is a JWT of our API key,
// signed with the secret, that carries the SHA-256 of body.
func (s *Service) verify(authorization string, body []byte) error {
	tok := strings.TrimPrefix(authorization, "Bearer ")
	if tok == "" {
		return errors.New("voice: webhook without Authorization")
	}
	v, err := lkauth.ParseAPIToken(tok)
	if err != nil {
		return fmt.Errorf("voice: parse webhook token: %w", err)
	}
	if v.APIKey() != s.key {
		return errors.New("voice: webhook token of another API key")
	}
	_, claims, err := v.Verify(s.secret)
	if err != nil {
		return fmt.Errorf("voice: verify webhook token: %w", err)
	}
	sum := sha256.Sum256(body)
	if subtle.ConstantTimeCompare([]byte(claims.Sha256), []byte(base64.StdEncoding.EncodeToString(sum[:]))) != 1 {
		return errors.New("voice: webhook body does not match its token")
	}
	return nil
}

// apply applies one verified event. LiveKit may repeat or reorder events,
// so each one is compared with the current state: a repeated event ID is
// dropped, and events about another participant or room SID than the
// state's change nothing.
func (s *Service) apply(ctx context.Context, ev *livekit.WebhookEvent) error {
	apply, err := s.beginEvent(ctx, ev.GetId())
	if err != nil {
		return err
	}
	if !apply {
		return nil
	}
	completed := false
	defer func() { s.finishEvent(ev.GetId(), completed) }()
	if err = s.applyEvent(ctx, ev); err != nil {
		return fmt.Errorf("voice: apply webhook: %w", err)
	}
	completed = true
	return nil
}

// applyEvent applies an event owned by this delivery, without holding the
// deduplication lock across permission reads.
func (s *Service) applyEvent(ctx context.Context, ev *livekit.WebhookEvent) error {
	channelID, ok := roomChannel(ev.GetRoom().GetName())
	if !ok {
		return nil
	}
	room := Room{SID: ev.GetRoom().GetSid(), Name: ev.GetRoom().GetName()}
	switch ev.GetEvent() {
	case eventJoined:
		return s.joined(ctx, channelID, room, participantOf(ev.GetParticipant()))
	case eventLeft, eventAborted:
		s.left(ctx, participantOf(ev.GetParticipant()))
	case eventPublish, eventUnpub:
		switch t := ev.GetTrack(); {
		case isScreen(t):
			s.screen(ctx, participantOf(ev.GetParticipant()), t.GetSid(), ev.GetEvent() == eventPublish)
		case isCamera(t):
			s.camera(ctx, participantOf(ev.GetParticipant()), t.GetSid(), ev.GetEvent() == eventPublish)
		}
	case eventFinished:
		s.finished(ctx, channelID, room.SID)
	}
	return nil
}

// beginEvent waits for an in-flight copy, or owns this delivery. Only a
// successful delivery is remembered; a failed one can be retried.
func (s *Service) beginEvent(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return true, nil
	}
	for {
		apply, done := s.claimEvent(id)
		if done == nil {
			return apply, nil
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("voice: wait for webhook delivery: %w", ctx.Err())
		case <-done:
		}
	}
}

// claimEvent claims an unseen delivery, or returns its current owner's completion.
func (s *Service) claimEvent(id string) (bool, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, at := range s.seen {
		if s.now().Sub(at) > seenTTL {
			delete(s.seen, k)
		}
	}
	if _, ok := s.seen[id]; ok {
		return false, nil
	}
	if done, ok := s.inflight[id]; ok {
		return false, done
	}
	s.inflight[id] = make(chan struct{})
	return true, nil
}

// finishEvent releases waiting deliveries and records only successful work.
func (s *Service) finishEvent(id string, success bool) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if success {
		s.seen[id] = s.now()
	}
	close(s.inflight[id])
	delete(s.inflight, id)
}

// admitted reports whether p may be in the room of channelID: the channel
// is a voice channel, the identity a member who may view it and CONNECT.
// It returns the guild of the channel. Errors are failures to find out.
func (s *Service) admitted(ctx context.Context, channelID snowflake.ID, p Participant) (snowflake.ID, permissions.Permission, bool, error) {
	userID, ok := identityUser(p.Identity)
	if !ok {
		return 0, 0, false, nil
	}
	c, err := s.channels.Lookup(ctx, channelID)
	if errors.Is(err, channels.ErrNotFound) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if c.Type != channels.TypeVoice && !c.Type.Private() {
		return 0, 0, false, nil
	}
	perm, guildID, err := s.channels.Permissions(ctx, userID, channelID)
	if errors.Is(err, channels.ErrNotFound) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return guildID, perm, perm.Has(permissions.Connect), nil
}

// joined handles participant_joined: an admitted participant whose token
// is of the user's latest join becomes the user's state, replacing one in
// another channel; any other participant is removed from the room.
func (s *Service) joined(ctx context.Context, channelID snowflake.ID, room Room, p Participant) error {
	userID, _ := identityUser(p.Identity)
	unlock, lockErr := s.lockUser(ctx, userID)
	if lockErr != nil {
		return lockErr
	}
	defer unlock()
	start, permissionRevision := s.admissionRevisions()
	guildID, mask, ok, err := s.admitted(ctx, channelID, p)
	if err != nil {
		return err
	}
	flags, current := s.prepareAdmission(ctx, guildID, userID, channelID, start, room, p, ok)
	if !current {
		return nil
	}
	err = s.applyPermissions(ctx, room.Name, p.Identity, mask, flags)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.removeLocked(room.Name, p.Identity, p.SID)
		return err
	}
	if !s.admissionCurrentLocked(ctx, guildID, userID, channelID, start, room, p) {
		return nil
	}
	// A same-SID webhook still applied fresh permissions remotely.
	s.touchLocked(userID)
	s.connectLocked(ctx, guildID, channelID, room, p)
	if s.permissionRev != permissionRevision {
		s.queueRefreshLocked(guildID)
	}
	return nil
}

// admissionRevisions captures revisions before permission reads.
func (s *Service) admissionRevisions() (uint64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev, s.permissionRev
}

// prepareAdmission validates the participant before applying remote grants.
func (s *Service) prepareAdmission(ctx context.Context, guildID, userID, channelID snowflake.ID, start uint64, room Room, p Participant, admitted bool) (serverFlags, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !admitted {
		s.removeLocked(room.Name, p.Identity, p.SID)
		return serverFlags{}, false
	}
	if !s.admissionCurrentLocked(ctx, guildID, userID, channelID, start, room, p) {
		return serverFlags{}, false
	}
	return s.moderation[memberKey{guildID: guildID, userID: userID}], true
}

// admissionCurrentLocked checks both sides of an admission RPC, so an
// intervening departure or committed removal cannot restore stale state.
func (s *Service) admissionCurrentLocked(ctx context.Context, guildID, userID, channelID snowflake.ID, start uint64, room Room, p Participant) bool {
	s.expireMovesLocked(ctx)
	if s.departedLocked(p.SID) {
		return false
	}
	if s.invalidatedLocked(guildID, userID, channelID, start) || !s.currentParticipantLocked(userID, channelID, p) {
		s.removeLocked(room.Name, p.Identity, p.SID)
		return false
	}
	return true
}

// currentLocked reports whether a participant with the join generation
// join may be userID in channelID: it is their latest join, or the service
// knows of none (it restarted since).
func (s *Service) currentLocked(userID, channelID snowflake.ID, join string) bool {
	in, ok := s.intents[userID]
	return !ok || (in.join != "" && in.join == join && in.channelID == channelID)
}

// connectLocked makes p the state of its user in channelID unless it
// already is. A state in another channel is dropped and its participant
// removed; in the same channel (a reconnect) the self flags carry over.
func (s *Service) connectLocked(ctx context.Context, guildID, channelID snowflake.ID, room Room, p Participant) {
	userID, _ := identityUser(p.Identity)
	// Successful admission supersedes a failed attempt's pending removal.
	key := removalKey{room: room.Name, identity: p.Identity}
	s.cancelRemovalLocked(key, p.SID)
	if move := s.moves[userID]; move != nil && move.channelID == channelID {
		delete(s.moves, userID)
	}
	old := s.sessions[userID]
	if old != nil && old.sid == p.SID {
		return
	}
	st := State{GuildID: guildID, ChannelID: channelID, UserID: userID, JoinedAt: p.JoinedAt,
		SelfStream: len(p.Screens) > 0, SelfVideo: len(p.Cameras) > 0}
	flags := s.moderation[memberKey{guildID: guildID, userID: userID}]
	st.Mute, st.Deaf = flags.mute, flags.deaf
	if st.JoinedAt.IsZero() {
		st.JoinedAt = s.now().UTC()
	}
	if old != nil && old.state.ChannelID == channelID {
		st.SelfMute, st.SelfDeaf = old.state.SelfMute, old.state.SelfDeaf
	} else if old != nil {
		s.dropLocked(ctx, userID, true)
	}
	sess := &session{state: st, room: room.Name, roomSID: room.SID, sid: p.SID, join: p.Join,
		screens: map[string]bool{}, cameras: map[string]bool{}}
	for _, sid := range p.Screens {
		sess.screens[sid] = true
	}
	for _, sid := range p.Cameras {
		sess.cameras[sid] = true
	}
	s.setLocked(ctx, sess)
}

// left handles participant_left and participant_connection_aborted: the
// state goes if it is of that participant.
func (s *Service) left(ctx context.Context, p Participant) {
	userID, ok := identityUser(p.Identity)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRemovalsLocked()
	if p.SID != "" {
		s.departed[p.SID] = s.now()
	}
	if sess := s.sessions[userID]; sess != nil && sess.sid == p.SID {
		s.dropLocked(ctx, userID, false)
	}
}

// screen handles a screen share track of p being published (on) or
// unpublished; self_stream holds while any is published.
func (s *Service) screen(ctx context.Context, p Participant, trackSID string, on bool) {
	userID, ok := identityUser(p.Identity)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[userID]
	if sess == nil || sess.sid != p.SID {
		return
	}
	if on {
		sess.screens[trackSID] = true
	} else {
		delete(sess.screens, trackSID)
	}
	if streaming := len(sess.screens) > 0; streaming != sess.state.SelfStream {
		sess.state.SelfStream = streaming
		s.touchLocked(userID)
		s.announceLocked(ctx, sess.state, false)
	}
}

// camera handles a camera track of p being published (on) or unpublished;
// self_video holds while any is published.
func (s *Service) camera(ctx context.Context, p Participant, trackSID string, on bool) {
	userID, ok := identityUser(p.Identity)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[userID]
	if sess == nil || sess.sid != p.SID {
		return
	}
	if sess.cameras == nil {
		sess.cameras = map[string]bool{}
	}
	if on {
		sess.cameras[trackSID] = true
	} else {
		delete(sess.cameras, trackSID)
	}
	if video := len(sess.cameras) > 0; video != sess.state.SelfVideo {
		sess.state.SelfVideo = video
		s.touchLocked(userID)
		s.announceLocked(ctx, sess.state, false)
	}
}

// finished handles room_finished: every state in that room (that SID; a
// room of the same name started since is another room) goes.
func (s *Service) finished(ctx context.Context, channelID snowflake.ID, roomSID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for userID, sess := range s.sessions {
		if sess.state.ChannelID == channelID && sess.roomSID == roomSID {
			s.dropLocked(ctx, userID, false)
		}
	}
}
