package voice_test

import (
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// Server mute belongs to a membership: someone kicked and invited back does
// not come back muted.
func TestRemovedMemberRejoinsWithoutServerMute(t *testing.T) {
	e, _, _ := moderationEnv(t, permissions.MuteMembers)
	on := true
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on}); err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	e.voice.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_2", "again")})
	st, ok := e.stateOf(7, 1)
	if !ok {
		t.Fatal("rejoined member has no voice state")
	}
	if st.Mute {
		t.Fatal("Mute = true after removal and rejoin, want false")
	}
}
