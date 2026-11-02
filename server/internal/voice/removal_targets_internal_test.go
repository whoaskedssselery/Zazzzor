package voice

import (
	"testing"
	"time"
)

func TestCoalescedRemovalPreservesRetryBudget(t *testing.T) {
	s := NewService(Options{Rooms: &replacementRoom{}, APIKey: "key", APISecret: "secret"})
	key := removalKey{room: "42", identity: "1"}
	s.removeLocked(key.room, key.identity, "PA_new")
	job := s.removals[key]
	job.attempts, job.due = 3, time.Now().Add(time.Minute)
	s.removeLocked(key.room, key.identity, "PA_new")
	if s.removals[key] != job {
		t.Fatal("duplicate SID replaced an in-flight removal")
	}
	s.removeLocked(key.room, key.identity, "PA_old")
	if merged := s.removals[key]; merged.attempts != job.attempts || !merged.due.Equal(job.due) {
		t.Fatal("coalescing reset the retry budget or deadline")
	}
}

func TestRemovalRetainsEveryRejectedSID(t *testing.T) {
	r := &replacementRoom{}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret"})
	s.mu.Lock()
	s.removeLocked("42", "1", "PA_new")
	s.removeLocked("42", "1", "PA_old")
	s.mu.Unlock()
	s.removeDue(t.Context())
	if !r.removed {
		t.Fatal("older removal replaced the rejected SID that is still connected")
	}
}

func TestStaleLocalStateDoesNotProtectRejectedParticipant(t *testing.T) {
	r := &replacementRoom{}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret"})
	s.mu.Lock()
	s.sessions[1] = &session{room: "42", sid: "PA_old"}
	s.removeLocked("42", "1", "PA_new")
	s.mu.Unlock()
	s.removeDue(t.Context())
	if !r.removed {
		t.Fatal("stale local state protected a remotely confirmed rejected SID")
	}
}

func TestAcceptedSIDCancellationKeepsOtherRejectedSIDs(t *testing.T) {
	r := &replacementRoom{}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret"})
	s.mu.Lock()
	s.removeLocked("42", "1", "PA_new")
	s.removeLocked("42", "1", "PA_old")
	s.cancelRemovalLocked(removalKey{room: "42", identity: "1"}, "PA_old")
	s.mu.Unlock()
	s.removeDue(t.Context())
	if !r.removed {
		t.Fatal("canceling one SID discarded another rejected participant")
	}
}
