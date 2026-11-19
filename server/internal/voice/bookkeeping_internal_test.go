package voice

import (
	"testing"
	"time"
)

func TestLateWebhookCannotRestoreRevocationAfterTokenHorizon(t *testing.T) {
	now := time.Now()
	s := NewService(Options{Now: func() time.Time { return now }, Rooms: &reconcileRooms{},
		Channels: retryGrantChannels{}, APIKey: "key", APISecret: "0123456789abcdef0123456789abcdef"})
	p := Participant{Identity: "1", SID: "old", Join: "generation", JoinedAt: now}
	if err := s.joined(t.Context(), 42, Room{Name: "42"}, p); err != nil {
		t.Fatalf("joined: %v", err)
	}
	if err := s.Leave(t.Context(), 1); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	now = now.Add(24 * time.Hour)
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if err := s.joined(t.Context(), 42, Room{Name: "42"}, p); err != nil {
		t.Fatalf("late joined: %v", err)
	}
	if len(s.sessions) != 0 {
		t.Errorf("late sessions after pruning = %d, want 0", len(s.sessions))
	}
}
