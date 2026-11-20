package voice_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// A LiveKit blip during the periodic reconcile must not end calls that are
// already connected: the grants are retried instead.
func TestReconcileKeepsConnectedParticipantWhenPermissionUpdateFails(t *testing.T) {
	t.Parallel()
	e, _, r := moderationEnv(t, permissions.MuteMembers)
	e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_1"})
	var calls atomic.Int32
	r.before = func(voice.ParticipantPermissions) error {
		if calls.Add(1) == 1 {
			return errFake
		}
		return nil
	}
	if err := e.voice.Reconcile(t.Context()); !errors.Is(err, errFake) {
		t.Fatalf("Reconcile error = %v, want the failed update", err)
	}
	if _, ok := e.stateOf(7, 1); !ok {
		t.Fatal("a failed permission update dropped a connected participant")
	}
	e.run()
	eventually(t, "the grants retried", func() bool { return calls.Load() >= 2 })
	if got := e.rooms.removals(); len(got) != 0 {
		t.Fatalf("removals = %v, want none", got)
	}
	if _, ok := e.stateOf(7, 1); !ok {
		t.Fatal("the retried grants dropped the participant")
	}
}
