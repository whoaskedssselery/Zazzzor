package voice

import (
	"context"
	"sync"
	"testing"
)

type screenRooms struct {
	*refreshRooms
	participants []Participant
	list         func()
}

func TestScreenReconcilePreservesConcurrentCompanion(t *testing.T) {
	s, r, room, p := screenService(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.rooms = &screenRooms{refreshRooms: r, participants: []Participant{p, {Identity: "1", SID: "PA_1", Join: "join"}},
		list: func() { close(entered); <-release }}
	go func() { done <- s.Reconcile(t.Context()) }()
	waitRefreshSignal(t, entered)
	newScreen := p
	newScreen.SID, newScreen.Screens = "SC_new", []string{"new"}
	if err := s.joined(t.Context(), 42, room, newScreen); err != nil {
		t.Fatalf("new screen: %v", err)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if s.sessions[1].companion == nil || s.sessions[1].companion.p.SID != newScreen.SID || !s.sessions[1].state.SelfStream {
		t.Fatal("stale listing displaced the concurrent companion")
	}
}

func TestScreenReconcilePreservesTrackDuringRPC(t *testing.T) {
	s, r, room, p := screenService(t)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("screen: %v", err)
	}
	s.rooms = &screenRooms{refreshRooms: r, participants: []Participant{p, {Identity: "1", SID: "PA_1", Join: "join"}}}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	r.before = func(identity string) error {
		if identity == p.Identity {
			close(entered)
			<-release
		}
		return nil
	}
	go func() { done <- s.Reconcile(t.Context()) }()
	waitRefreshSignal(t, entered)
	s.screen(t.Context(), p, "new", true)
	unblock()
	if err := <-done; err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !s.sessions[1].state.SelfStream {
		t.Fatal("stale RPC completion erased a published screen")
	}
}

func (*screenRooms) ListRooms(context.Context) ([]Room, error) {
	return []Room{{Name: "42", SID: "RM_1"}}, nil
}

func (r *screenRooms) ListParticipants(context.Context, string) ([]Participant, error) {
	if r.list != nil {
		r.list()
	}
	return r.participants, nil
}

func TestScreenReconcileRestoresOwnerBeforeCompanion(t *testing.T) {
	s, r, _, p := screenService(t)
	p.Screens = []string{"track"}
	s.rooms = &screenRooms{refreshRooms: r, participants: []Participant{p, {Identity: "1", SID: "PA_1", Join: "join"}}}
	delete(s.sessions, 1)
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(s.sessions) != 1 || s.sessions[1] == nil || !s.sessions[1].state.SelfStream {
		t.Fatal("restart did not restore companion stream under its owner")
	}
	if s.removals[removalKey{"42", p.Identity}] != nil {
		t.Fatal("valid companion rejected by reconcile")
	}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if !s.sessions[1].state.SelfStream {
		t.Fatal("second reconcile lost companion tracks")
	}
}

func TestScreenReconcileRemovesOrphan(t *testing.T) {
	s, r, _, p := screenService(t)
	s.rooms = &screenRooms{refreshRooms: r, participants: []Participant{p}}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if s.sessions[1] != nil || s.removals[removalKey{"42", p.Identity}] == nil {
		t.Fatal("orphan was kept")
	}
}

func TestScreenReconcileClearsMissingCompanion(t *testing.T) {
	s, r, room, p := screenService(t)
	p.Screens = []string{"track"}
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("admission: %v", err)
	}
	s.rooms = &screenRooms{refreshRooms: r, participants: []Participant{{Identity: "1", SID: "PA_1", Join: "join"}}}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if s.sessions[1].companion != nil || s.sessions[1].state.SelfStream {
		t.Fatal("missing companion remained public")
	}
}

type screenMultiRooms struct {
	*screenRooms
	orphanFirst bool
}

func (r *screenMultiRooms) ListRooms(context.Context) ([]Room, error) {
	rooms := []Room{{Name: "42", SID: "RM_1"}, {Name: "43", SID: "RM_orphan"}}
	if r.orphanFirst {
		rooms[0], rooms[1] = rooms[1], rooms[0]
	}
	return rooms, nil
}

func (r *screenMultiRooms) ListParticipants(ctx context.Context, room string) ([]Participant, error) {
	if room == "43" {
		return []Participant{{Identity: "1.screen", SID: "SC_orphan", Join: "join"}}, nil
	}
	return r.screenRooms.ListParticipants(ctx, room)
}

func TestScreenReconcileRemovesOrphanAlongsideValidCompanion(t *testing.T) {
	for _, orphanFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid first", true: "orphan first"}[orphanFirst], func(t *testing.T) {
			s, r, _, p := screenService(t)
			p.Screens = []string{"valid"}
			s.rooms = &screenMultiRooms{screenRooms: &screenRooms{refreshRooms: r,
				participants: []Participant{{Identity: "1", SID: "PA_1", Join: "join"}, p}}, orphanFirst: orphanFirst}
			delete(s.sessions, 1)
			for range 2 {
				if err := s.Reconcile(t.Context()); err != nil {
					t.Fatalf("Reconcile: %v", err)
				}
				if s.removals[removalKey{"43", "1.screen"}] == nil {
					t.Fatal("valid admission hid another room's orphan")
				}
				if sess := s.sessions[1]; sess == nil || sess.companion == nil || sess.companion.p.SID != p.SID || !sess.state.SelfStream {
					t.Fatal("orphan cleanup removed the valid companion")
				}
			}
		})
	}
}
