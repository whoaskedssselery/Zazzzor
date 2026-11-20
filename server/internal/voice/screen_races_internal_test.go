package voice

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func TestScreenAdmissionCannotRestoreEndedOwner(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"permission read", "grants RPC"} {
		for _, cause := range []string{"leave", "departure", "replacement", "disabled", "screen departed"} {
			t.Run(phase+"/"+cause, func(t *testing.T) {
				s, r, room, p := screenService(t)
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
				unblock := sync.OnceFunc(func() { close(release) })
				defer unblock()
				block := func() { close(entered); <-release }
				if phase == "permission read" {
					s.channels = refreshChannels{read: func(context.Context, snowflake.ID) (permissions.Permission, error) {
						block()
						return permissions.ViewChannel | permissions.Connect | permissions.Stream, nil
					}}
				} else {
					r.before = func(string) error { block(); return nil }
				}
				go func() { done <- s.joined(t.Context(), 42, room, p) }()
				waitRefreshSignal(t, entered)
				switch cause {
				case "leave":
					if err := s.Leave(t.Context(), 1); err != nil {
						t.Fatalf("Leave: %v", err)
					}
				case "departure":
					s.left(t.Context(), Participant{Identity: "1", SID: "PA_1"})
				case "replacement":
					s.mu.Lock()
					s.connectLocked(t.Context(), 7, 42, room, Participant{Identity: "1", SID: "PA_new", Join: "new"})
					s.mu.Unlock()
				case "disabled":
					s.RemoveUser(t.Context(), 1)
				case "screen departed":
					s.left(t.Context(), p)
				}
				unblock()
				if err := <-done; err != nil {
					t.Fatalf("admission: %v", err)
				}
				if sess := s.sessions[1]; sess != nil && sess.companion != nil {
					t.Fatal("stale admission restored companion")
				}
				if s.removals[removalKey{room.Name, p.Identity}] == nil {
					t.Fatal("stale admission was not removed")
				}
				if _, updated := r.updates[p.Identity]; phase == "permission read" && updated {
					t.Fatal("ended binding reached the grants RPC")
				}
			})
		}
	}
}

func TestScreenObservationWaitsForStripeWithoutOverwritingTrack(t *testing.T) {
	t.Parallel()
	s, r, room, p := screenService(t)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("admission: %v", err)
	}
	observed := &screenObservation{sess: s.sessions[1], revision: s.revs[1]}
	unlock, err := s.lockUser(t.Context(), 1)
	if err != nil {
		t.Fatalf("lockUser: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.joinedScreen(t.Context(), 42, room, p, observed) }()
	s.screen(t.Context(), p, "new", true)
	delete(r.updates, p.Identity)
	unlock()
	if err := <-done; err != nil {
		t.Fatalf("reconcile admission: %v", err)
	}
	if _, updated := r.updates[p.Identity]; updated || !s.sessions[1].state.SelfStream {
		t.Fatal("stale observation reached grants or erased a new track")
	}
}

func TestScreenTokenCannotOutliveOwnerDuringPermissionRead(t *testing.T) {
	t.Parallel()
	s, _, _, _ := screenService(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.channels = refreshChannels{read: func(context.Context, snowflake.ID) (permissions.Permission, error) {
		close(entered)
		<-release
		return permissions.ViewChannel | permissions.Connect | permissions.Stream, nil
	}}
	go func() { _, err := s.JoinScreen(t.Context(), 42, auth.User{ID: 1}, "https://zazzzor.test"); done <- err }()
	waitRefreshSignal(t, entered)
	if err := s.Leave(t.Context(), 1); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	unblock()
	if err := <-done; !errors.Is(err, ErrNotInVoice) {
		t.Fatalf("JoinScreen error = %v, want not in voice", err)
	}
}

func TestScreenRefreshRevokesBeforeFailingPrimaryRPC(t *testing.T) {
	t.Parallel()
	for _, lost := range []permissions.Permission{permissions.Stream, permissions.Connect} {
		t.Run(map[permissions.Permission]string{permissions.Stream: "stream", permissions.Connect: "connect"}[lost], func(t *testing.T) {
			s, r, room, p := screenService(t)
			if err := s.joined(t.Context(), 42, room, p); err != nil {
				t.Fatalf("admission: %v", err)
			}
			s.channels = refreshChannels{read: func(context.Context, snowflake.ID) (permissions.Permission, error) {
				return (permissions.ViewChannel | permissions.Connect | permissions.Stream) &^ lost, nil
			}}
			r.before = func(string) error { return errors.New("primary RPC unavailable") }
			s.RefreshGuild(7)
			s.refreshDue(t.Context())
			if s.sessions[1].companion != nil || s.removals[removalKey{room.Name, p.Identity}] == nil {
				t.Fatal("primary RPC failure prevented companion revocation")
			}
		})
	}
}

func TestScreenStaleRemovalProtectsRejoinedCompanion(t *testing.T) {
	t.Parallel()
	s, _, room, p := screenService(t)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("admission: %v", err)
	}
	s.removeLocked(room.Name, p.Identity, "SC_old")
	key := removalKey{room.Name, p.Identity}
	if !s.rejoinedLocked(key, s.removals[key]) {
		t.Fatal("stale companion removal would disconnect the accepted SID")
	}
}

type screenRemovalRooms struct {
	*refreshRooms
	listed, release chan struct{}
}

func (r *screenRemovalRooms) ListParticipants(context.Context, string) ([]Participant, error) {
	close(r.listed)
	<-r.release
	return nil, nil
}

func TestScreenRemovalRechecksCompanionAfterRemoteListing(t *testing.T) {
	t.Parallel()
	s, r, room, p := screenService(t)
	listed, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.rooms = &screenRemovalRooms{refreshRooms: r, listed: listed, release: release}
	s.removeLocked(room.Name, p.Identity, "SC_old")
	key := removalKey{room.Name, p.Identity}
	job := s.removals[key]
	go func() { done <- s.removeParticipant(t.Context(), key, job) }()
	waitRefreshSignal(t, listed)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("new admission: %v", err)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatalf("removeParticipant: %v", err)
	}
	if r.removed[p.Identity] {
		t.Fatal("in-flight stale removal disconnected rejoined companion")
	}
}
