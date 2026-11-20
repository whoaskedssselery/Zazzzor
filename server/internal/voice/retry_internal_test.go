package voice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type observedWait struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *observedWait) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestRemovalTombstonesExpireWithoutJoins(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := NewService(Options{Now: func() time.Time { return now }})
	s.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
	s.RemoveChannel(t.Context(), 42)
	s.left(t.Context(), Participant{SID: "gone", Identity: "1"})
	now = now.Add(seenTTL + time.Second)
	s.RemoveMembers(t.Context(), 7, []snowflake.ID{2})
	s.RemoveChannel(t.Context(), 43)
	s.left(t.Context(), Participant{SID: "new", Identity: "2"})
	if len(s.membersRemoved) != 1 || len(s.channelsRemoved) != 1 || len(s.departed) != 1 || len(s.revs) != 0 {
		t.Fatalf("retained removal state: members=%d channels=%d departed=%d revisions=%d",
			len(s.membersRemoved), len(s.channelsRemoved), len(s.departed), len(s.revs))
	}
}

func TestConcurrentDeliveryWaitsForOutcome(t *testing.T) {
	t.Parallel()
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			s := NewService(Options{})
			apply, err := s.beginEvent(t.Context(), "event")
			if err != nil || !apply {
				t.Fatalf("first delivery: %v %v", apply, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			observed := &observedWait{Context: ctx, entered: make(chan struct{})}
			done := make(chan bool, 1)
			go func() { next, _ := s.beginEvent(observed, "event"); done <- next }()
			select {
			case <-observed.entered:
			case <-time.After(5 * time.Second):
				cancel()
				t.Fatal("duplicate did not wait")
			}
			s.finishEvent("event", success)
			select {
			case next := <-done:
				if next == success {
					t.Fatalf("retry ownership = %v after success=%v", next, success)
				}
				if next {
					s.finishEvent("event", true)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("duplicate delivery stuck")
			}
			cancel()
		})
	}
}

func TestDuplicateWaitCanBeCancelled(t *testing.T) {
	t.Parallel()
	s := NewService(Options{})
	_, _ = s.beginEvent(t.Context(), "event")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if apply, err := s.beginEvent(ctx, "event"); apply || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v, %v", apply, err)
	}
	s.finishEvent("event", true)
}
