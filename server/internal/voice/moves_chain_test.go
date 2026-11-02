package voice_test

import (
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// A -> B (B token issued) -> C, then back to A: the B token must not connect.
func TestChainedMoveBackRefusesFirstDestinationToken(t *testing.T) {
	e, _, _, _ := pendingMoveEnv(t)
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("destination Join: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(45)}); err != nil {
		t.Fatalf("second move: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(42)}); err != nil {
		t.Fatalf("move back: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_dest"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_dest", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 42 {
		t.Fatalf("state = %+v, want channel 42 after the chain was cancelled", st)
	}
}

// Same, but C is deleted instead of a move back.
func TestChainedMoveToDeletedChannelRefusesFirstDestinationToken(t *testing.T) {
	e, _, _, _ := pendingMoveEnv(t)
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("destination Join: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(45)}); err != nil {
		t.Fatalf("second move: %v", err)
	}
	e.voice.RemoveChannel(t.Context(), 45)
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_dest"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_dest", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 42 {
		t.Fatalf("state = %+v, want channel 42 after the chain was cancelled", st)
	}
}

// Without a prior intent (the API restarted), cancelling still pins the
// source's join, so the destination token is refused.
func TestCancelledMoveWithoutPriorIntentRefusesDestinationToken(t *testing.T) {
	e, _, _ := moderationEnv(t, permissions.All)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("move: %v", err)
	}
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
}
