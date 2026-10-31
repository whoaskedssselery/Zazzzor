package voice

import (
	"context"
	"testing"
)

type replacementRoom struct{ removed bool }

func (*replacementRoom) UpdateParticipant(context.Context, string, string, ParticipantPermissions) error {
	return nil
}

func (*replacementRoom) ListRooms(context.Context) ([]Room, error) { return nil, nil }
func (*replacementRoom) ListParticipants(context.Context, string) ([]Participant, error) {
	return []Participant{{SID: "PA_new", Identity: "1"}}, nil
}
func (r *replacementRoom) RemoveParticipant(context.Context, string, string) error {
	r.removed = true
	return nil
}

func TestQueuedRemovalPreservesRemoteReplacement(t *testing.T) {
	r := &replacementRoom{}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret"})
	s.mu.Lock()
	s.removeLocked("42", "1", "PA_old")
	s.mu.Unlock()
	s.removeDue(t.Context())
	if r.removed {
		t.Fatal("removed replacement before its join webhook arrived")
	}
}
