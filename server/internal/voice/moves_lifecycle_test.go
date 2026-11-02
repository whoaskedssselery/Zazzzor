package voice_test

import (
	"errors"
	"slices"
	"sync"
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

func TestModeratorDisconnectRevokesPendingTokens(t *testing.T) {
	e, _, r := moderationEnv(t, permissions.MoveMembers)
	old, err := e.voice.Join(t.Context(), 42, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: disconnect()}); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if len(e.voice.GuildStates(7)) != 0 || !slices.Contains(r.removals(), "42/1") {
		t.Fatalf("states/removals = %v/%v, want none/42/1", e.voice.GuildStates(7), r.removals())
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_stale", claims(t, old.Token).Attributes["zazzzor.join"])})
	if len(e.voice.GuildStates(7)) != 0 {
		t.Fatalf("states = %v, want none after a pre-disconnect token", e.voice.GuildStates(7))
	}
}

func TestVoiceMoveSupersedesAnOlderDestinationToken(t *testing.T) {
	e, _, _ := moderationEnv(t, permissions.MoveMembers)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("first Move: %v", err)
	}
	old, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("first target token: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(45)}); err != nil {
		t.Fatalf("second Move: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_old"), Participant: lkParticipant(userBody{ID: "1"}, "PA_old_target", claims(t, old.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 42 {
		t.Fatalf("obsolete destination changed source=%+v", st)
	}
	j, err := e.voice.Join(t.Context(), 45, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("new target token: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_1", "")})
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("45", "RM_new"), Participant: lkParticipant(userBody{ID: "1"}, "PA_new_target", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 45 {
		t.Fatalf("latest destination state=%+v", st)
	}
}

func TestVoiceMoveRejectsNewSourceSessions(t *testing.T) {
	e, _, _ := moderationEnv(t, permissions.MoveMembers)
	old, err := e.voice.Join(t.Context(), 42, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("source token: %v", err)
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_rejected_source", claims(t, old.Token).Attributes["zazzzor.join"])})
	e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_rejected_source", Join: claims(t, old.Token).Attributes["zazzzor.join"]})
	e.run()
	e.rooms.waitRemoved(t, "42/1")
}

func TestExplicitJoinElsewhereCancelsTheMove(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	e, _, r := moderationEnv(t, permissions.MoveMembers, func(o *voice.Options) { o.Now = func() time.Time { return time.Unix(0, clock.Load()) } })
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	j, err := e.voice.Join(t.Context(), 45, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("explicit Join: %v", err)
	}
	if !slices.Contains(r.removals(), "42/1") {
		t.Fatalf("removals = %v, want 42/1 after an explicit join", r.removals())
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("45", "RM_elsewhere"), Participant: lkParticipant(userBody{ID: "1"}, "PA_elsewhere", claims(t, j.Token).Attributes["zazzzor.join"])})
	e.rooms.put(voice.Room{Name: "45", SID: "RM_elsewhere"}, voice.Participant{Identity: "1", SID: "PA_elsewhere", Join: claims(t, j.Token).Attributes["zazzzor.join"]})
	clock.Add(int64(20 * time.Second))
	e.reconcile()
	if st, _ := e.stateOf(7, 1); st.ChannelID != 45 || slices.Contains(r.removals(), "45/1") {
		t.Fatalf("old fallback touched newer session=%+v", st)
	}
}

func TestDisconnectChecksMoveAndPreservesStateOnFailure(t *testing.T) {
	e, c, r := moderationEnv(t, permissions.MuteMembers|permissions.DeafenMembers)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: disconnect()}); !errors.Is(err, voice.ErrMissingPerm) {
		t.Fatalf("disconnect without MOVE=%v", err)
	}
	c.actor |= permissions.MoveMembers
	r.failRemovals = 1
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: disconnect()}); !errors.Is(err, voice.ErrUnavailable) {
		t.Fatalf("failed disconnect=%v", err)
	}
	if st, ok := e.stateOf(7, 1); !ok {
		t.Fatalf("state = %+v/%v, want kept after a failed disconnect", st, ok)
	}
}

func TestDisconnectHandlesItsOwnLeftWebhook(t *testing.T) {
	e, _, r := moderationEnv(t, permissions.MoveMembers)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("target token: %v", err)
	}
	r.beforeRemove = func() {
		e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_1", "")})
	}
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: disconnect()}); err != nil {
		t.Fatalf("disconnect with left webhook: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_target"), Participant: lkParticipant(userBody{ID: "1"}, "PA_revoked", claims(t, j.Token).Attributes["zazzzor.join"])})
	if len(e.voice.GuildStates(7)) != 0 {
		t.Fatalf("states = %v, want none after a revoked destination token", e.voice.GuildStates(7))
	}
}

func TestMoveToCurrentChannelIsNoop(t *testing.T) {
	e, _, r := moderationEnv(t, permissions.MoveMembers)
	n, updates := len(e.pub.events), len(r.updates)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(42)}); err != nil {
		t.Fatalf("same-channel Move: %v", err)
	}
	if len(e.pub.events) != n || len(r.updates) != updates || len(r.removals()) != 0 {
		t.Fatalf("events/updates/removals = +%d/+%d/%v, want no effects", len(e.pub.events)-n, len(r.updates)-updates, r.removals())
	}
}

func TestMoveAndMuteAppliesFlagsToTheDestination(t *testing.T) {
	e, _, _ := moderationEnv(t, permissions.MoveMembers|permissions.MuteMembers)
	on := true
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on, Move: destination(44)}); err != nil {
		t.Fatalf("move and mute: %v", err)
	}
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("target token: %v", err)
	}
	if sources := claims(t, j.Token).Video.CanPublishSources; slices.Contains(sources, "microphone") {
		t.Fatalf("destination token sources = %v, want no microphone", sources)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_target"), Participant: lkParticipant(userBody{ID: "1"}, "PA_muted", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); !st.Mute || st.ChannelID != 44 {
		t.Fatalf("moved muted state=%+v", st)
	}
}

func TestMovePreparationDoesNotOutliveMemberRemoval(t *testing.T) {
	e, c, _ := moderationEnv(t, permissions.MoveMembers)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	c.afterRead = func(user snowflake.ID) {
		if user == 1 {
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)})
	}()
	awaitSignal(t, entered)
	e.voice.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
	unblock()
	if err := awaitError(t, done); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("removed participant move=%v, want not found", err)
	}
	for _, ev := range e.pub.events {
		if ev.Type == events.VoiceMove {
			t.Fatalf("event = %+v, want no VOICE_MOVE for a removed participant", ev)
		}
	}
}
