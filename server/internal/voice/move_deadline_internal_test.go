package voice

import (
	"testing"
	"time"
)

func TestMoveDeadlineIsDrivenByRemovalWorker(t *testing.T) {
	now := time.Now()
	r := &replacementRoom{}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret", Now: func() time.Time { return now }})
	source := &session{room: "42", sid: "PA_new", state: State{GuildID: 7, ChannelID: 42, UserID: 1}}
	s.mu.Lock()
	s.sessions[1] = source
	s.requestMoveLocked(t.Context(), 1, 44, source)
	s.mu.Unlock()
	next, ok := s.removeDue(t.Context())
	if !ok || !next.Equal(now.Add(15*time.Second)) || r.removed {
		t.Fatalf("worker next=%v (%v), removed=%v", next, ok, r.removed)
	}
	now = now.Add(15 * time.Second)
	s.removeDue(t.Context())
	if !r.removed || len(s.GuildStates(7)) != 0 {
		t.Fatal("worker did not expire the captured source")
	}
}
