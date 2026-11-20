package voice_test

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// pendingMoveEnv connects user 1 to channel 42 with a token of their latest
// join, as LiveKit lists them, and moves them towards 44 on a clock the
// test advances.
func pendingMoveEnv(t *testing.T) (*env, *moderationChannels, *moderationRooms, *atomic.Int64) {
	t.Helper()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	e, c, r := moderationEnv(t, permissions.MoveMembers, func(o *voice.Options) {
		o.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	})
	j, err := e.voice.Join(t.Context(), 42, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("source Join: %v", err)
	}
	join := claims(t, j.Token).Attributes["zazzzor.join"]
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_src", join)})
	e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_src", Join: join})
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	return e, c, r, &clock
}

// expectSourceKept passes the move deadline and checks that user 1 is
// still in channel 42 and was not removed from it.
func expectSourceKept(t *testing.T, e *env, r *moderationRooms, clock *atomic.Int64) {
	t.Helper()
	clock.Add(int64(voice.MoveTimeout + 5*time.Second))
	e.reconcile()
	st, ok := e.stateOf(7, 1)
	if !ok || st.ChannelID != 42 {
		t.Fatalf("state after the deadline = %+v (%v), want channel 42", st, ok)
	}
	if slices.Contains(r.removals(), "42/1") {
		t.Fatalf("removals = %v, want the source kept", r.removals())
	}
}

// lastMove returns the destination of the last VOICE_MOVE sent to user 1.
func lastMove(e *env) snowflake.ID {
	e.pub.mu.Lock()
	defer e.pub.mu.Unlock()
	for i := len(e.pub.events) - 1; i >= 0; i-- {
		if ev := e.pub.events[i]; ev.Type == events.VoiceMove && slices.Equal(ev.Audience.UserIDs, []snowflake.ID{1}) {
			return ev.Payload.(voice.MovePayload).ChannelID
		}
	}
	return 0
}

func TestMoveBackCancelsPendingMove(t *testing.T) {
	t.Parallel()
	e, _, r, clock := pendingMoveEnv(t)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(42)}); err != nil {
		t.Fatalf("move back: %v", err)
	}
	if got := lastMove(e); got != 42 {
		t.Fatalf("last VOICE_MOVE = %v, want 42 (back to the source)", got)
	}
	expectSourceKept(t, e, r, clock)
}

func TestMoveBackRefusesAnIssuedDestinationToken(t *testing.T) {
	t.Parallel()
	e, _, r, _ := pendingMoveEnv(t)
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("destination Join: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(42)}); err != nil {
		t.Fatalf("move back: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_dest"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_dest", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 42 {
		t.Fatalf("state = %+v, want channel 42 after the move was cancelled", st)
	}
	e.run()
	r.waitRemoved(t, "44/1")
}

func TestDeletedDestinationCancelsPendingMove(t *testing.T) {
	t.Parallel()
	e, _, r, clock := pendingMoveEnv(t)
	e.voice.RemoveChannel(t.Context(), 44)
	if got := lastMove(e); got != 42 {
		t.Fatalf("last VOICE_MOVE = %v, want 42 (back to the source)", got)
	}
	expectSourceKept(t, e, r, clock)
}

func TestLostDestinationConnectCancelsPendingMove(t *testing.T) {
	t.Parallel()
	e, c, r, clock := pendingMoveEnv(t)
	c.targetChannels = map[snowflake.ID]permissions.Permission{44: permissions.ViewChannel}
	e.voice.RefreshGuild(7)
	e.run()
	eventually(t, "the move to be cancelled", func() bool { return lastMove(e) == 42 })
	expectSourceKept(t, e, r, clock)
}

func TestDestinationDeletedDuringMoveCheckIsNotFound(t *testing.T) {
	t.Parallel()
	e, c, _ := moderationEnv(t, permissions.MoveMembers)
	c.afterRead = func(user snowflake.ID) {
		if user == 1 {
			e.voice.RemoveChannel(t.Context(), 44)
		}
	}
	err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)})
	if !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("Moderate error = %v, want not found", err)
	}
	if got := lastMove(e); got != 0 {
		t.Fatalf("last VOICE_MOVE = %v, want none", got)
	}
}
