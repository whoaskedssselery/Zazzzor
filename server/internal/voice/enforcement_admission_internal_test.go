package voice

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type discoveredRefreshRooms struct{ *refreshRooms }

func (*discoveredRefreshRooms) ListRooms(context.Context) ([]Room, error) {
	return []Room{{Name: "42", SID: "RM_1"}}, nil
}

func (*discoveredRefreshRooms) ListParticipants(context.Context, string) ([]Participant, error) {
	return []Participant{{Identity: "1", SID: "PA_1"}}, nil
}

func TestRefreshCoversAdmissionNotYetInLocalState(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		t.Run(map[bool]string{false: "webhook", true: "reconcile"}[reconcile], func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var reads atomic.Int64
			s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				if reads.Add(1) == 1 {
					close(entered)
					<-release
					return permissions.ViewChannel | permissions.Connect | permissions.Speak, nil
				}
				return permissions.ViewChannel, nil
			})
			s.rooms = &discoveredRefreshRooms{refreshRooms: r}
			delete(s.sessions, 1)
			var err error
			go func() {
				if reconcile {
					err = s.Reconcile(t.Context())
				} else {
					err = s.joined(t.Context(), 42, Room{Name: "42", SID: "RM_1"}, Participant{Identity: "1", SID: "PA_1"})
				}
				close(done)
			}()
			waitRefreshSignal(t, entered)
			s.RefreshGuild(7) // No local session exists while the old read is blocked.
			s.refreshDue(t.Context())
			unblock()
			waitRefreshSignal(t, done)
			if err != nil {
				t.Fatalf("admission: %v", err)
			}
			s.refreshDue(t.Context())
			if s.sessions[1] != nil || !r.removed["1"] {
				t.Fatal("admission missed the permission change while no state existed")
			}
		})
	}
}
