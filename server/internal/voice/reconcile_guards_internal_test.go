package voice

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockedReconcileRoom struct {
	reconcileRooms
	entered, release chan struct{}
}

func (r *blockedReconcileRoom) ListParticipants(ctx context.Context, room string) ([]Participant, error) {
	close(r.entered)
	select {
	case <-r.release:
		return r.reconcileRooms.ListParticipants(ctx, room)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestReconcileKeepsJoinCommittedDuringListing(t *testing.T) {
	r := &blockedReconcileRoom{
		reconcileRooms: reconcileRooms{rooms: []Room{{Name: "42", SID: "RM_1"}},
			participants: map[string][]Participant{"42": {{Identity: "1", SID: "PA_old"}}}},
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	unblock := sync.OnceFunc(func() { close(r.release) })
	defer unblock()
	s := NewService(Options{Rooms: r, Channels: retryGrantChannels{}, APIKey: "key", APISecret: "secret"})
	done := make(chan error, 1)
	go func() { done <- s.Reconcile(t.Context()) }()
	waitRefreshSignal(t, r.entered)
	if err := s.joined(t.Context(), 42, Room{Name: "42", SID: "RM_1"}, Participant{Identity: "1", SID: "PA_new"}); err != nil {
		t.Fatalf("new join: %v", err)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not finish")
	}
	if current := s.sessions[1]; current == nil || current.sid != "PA_new" {
		t.Fatalf("state after old snapshot = %+v, want newest SID", current)
	}
}

func TestRemovalKeepsLocalRejoinMissingFromRemoteListing(t *testing.T) {
	r := &reconcileRooms{}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret"})
	s.sessions[1] = &session{room: "42", sid: "PA_new"}
	s.removeLocked("42", "1", "PA_old")
	s.removeDue(t.Context())
	if r.removed {
		t.Fatal("queued old removal disconnected the known local rejoin")
	}
}

// failingRemovalRoom fails every removal until healed is set.
type failingRemovalRoom struct {
	replacementRoom
	attempts int
	healed   bool
}

func (r *failingRemovalRoom) RemoveParticipant(context.Context, string, string) error {
	r.attempts++
	if r.healed {
		return nil
	}
	return errors.New("media unavailable")
}

func TestRemovalOutageWarnsPeriodicallyAndLogsRecovery(t *testing.T) {
	r := &failingRemovalRoom{}
	var logs bytes.Buffer
	now := time.Now()
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret", Now: func() time.Time { return now },
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	s.removeLocked("42", "1", "PA_new")
	for range removeAttempts + removeWarnEvery {
		s.removeDue(t.Context())
		now = now.Add(time.Hour)
	}
	if got := strings.Count(logs.String(), "could not remove a participant"); got != 2 {
		t.Fatalf("retry warnings = %d, want 2: %s", got, logs.String())
	}
	r.healed = true
	s.removeDue(t.Context())
	if len(s.removals) != 0 || !strings.Contains(logs.String(), "removed a participant from LiveKit after retries") {
		t.Fatalf("pending=%d log=%s, want 0 and a recovery line", len(s.removals), logs.String())
	}
}

func TestRemovalContinuesAfterFiveFailuresAndLogsOnce(t *testing.T) {
	r := &failingRemovalRoom{}
	var logs bytes.Buffer
	now := time.Now()
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret", Now: func() time.Time { return now },
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	s.removeLocked("42", "1", "PA_new")
	for range 5 {
		s.removeDue(t.Context())
		now = now.Add(time.Hour)
	}
	s.removeDue(t.Context())
	if r.attempts != 6 || len(s.removals) != 1 || strings.Count(logs.String(), "could not remove a participant") != 1 {
		t.Fatalf("removal attempts=%d pending=%d log=%s, want 6/1/one retry warning", r.attempts, len(s.removals), logs.String())
	}
	for _, field := range []string{`"room":"42"`, `"identity":"1"`, `"participant":"PA_new"`, `"attempts":5`} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("retry warning missing %s: %s", field, logs.String())
		}
	}
}
