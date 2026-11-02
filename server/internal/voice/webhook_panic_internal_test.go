package voice

import (
	"context"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// panicPublisher panics on the next Publish, which runs under s.mu.
type panicPublisher struct{ panicNext bool }

func (p *panicPublisher) Publish(context.Context, events.Event) {
	if p.panicNext {
		p.panicNext = false
		panic("publish under state lock")
	}
}

func TestWebhookLockedPublishPanicReleasesStateAndDelivery(t *testing.T) {
	s, _ := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		return permissions.ViewChannel | permissions.Connect, nil
	})
	s.pub = &panicPublisher{true}
	s.moves[1] = &pendingMove{source: s.sessions[1], channelID: 43, deadline: s.now().Add(-time.Second)}
	ev := &livekit.WebhookEvent{Id: "locked_publish", Event: eventJoined, Room: &livekit.Room{Name: "42", Sid: "RM_1"}, Participant: &livekit.ParticipantInfo{Identity: "2", Sid: "PA_2"}}
	done := make(chan bool, 1)
	go func() {
		panicked := false
		defer func() {
			if recover() != nil {
				panicked = true
			}
			done <- panicked
		}()
		_ = s.apply(t.Context(), ev)
	}()
	select {
	case panicked := <-done:
		if !panicked {
			t.Fatal("publish panic = false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("webhook completion timed out, want panic to release state and delivery locks")
	}
	if len(s.inflight) != 0 || len(s.seen) != 0 {
		t.Fatalf("ownership = %d/%d, want 0/0", len(s.inflight), len(s.seen))
	}
	if err := s.apply(t.Context(), ev); err != nil {
		t.Fatalf("retry after panic: %v", err)
	}
	if got := len(s.GuildStates(7)); got != 1 {
		t.Fatalf("state count = %d, want 1", got)
	}
}

func TestWebhookDestinationAdmissionClearsCompletedMove(t *testing.T) {
	s, _ := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		return permissions.ViewChannel | permissions.Connect, nil
	})
	s.moves[1] = &pendingMove{source: s.sessions[1], channelID: 43, deadline: s.now().Add(MoveTimeout)}
	s.left(t.Context(), Participant{Identity: "1", SID: s.sessions[1].sid})
	if err := s.joined(t.Context(), 43, Room{Name: "43", SID: "RM_43"}, Participant{Identity: "1", SID: "PA_dest"}); err != nil {
		t.Fatalf("admission: %v", err)
	}
	if got := s.moves[1]; got != nil {
		t.Fatalf("completed move = %+v, want nil", got)
	}
	if err := s.joined(t.Context(), 44, Room{Name: "44", SID: "RM_44"}, Participant{Identity: "1", SID: "PA_later"}); err != nil {
		t.Fatalf("later admission: %v", err)
	}
	if got := s.sessions[1].state.ChannelID; got != 44 {
		t.Fatalf("later channel = %v, want 44", got)
	}
}
