package voice

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// waitRefreshSignal bounds blocking fake handshakes without sleeping.
func waitRefreshSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("permission refresh handshake timed out")
	}
}

func TestRefreshKeepsSyncArrivingDuringWork(t *testing.T) {
	for _, duringRPC := range []bool{false, true} {
		t.Run(map[bool]string{false: "permission_read", true: "media_rpc"}[duringRPC], func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var reads atomic.Int64
			s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				first := reads.Add(1) == 1
				if first && !duringRPC {
					close(entered)
					<-release
				}
				p := permissions.ViewChannel | permissions.Connect
				if !first {
					p |= permissions.Stream
				}
				return p, nil
			})
			if duringRPC {
				var block sync.Once
				r.before = func(string) error {
					block.Do(func() { close(entered); <-release })
					return nil
				}
			}
			s.RefreshGuild(7)
			go func() { s.refreshDue(t.Context()); close(done) }()
			waitRefreshSignal(t, entered)
			s.RefreshGuild(7)
			unblock()
			waitRefreshSignal(t, done)
			if reads.Load() != 2 || len(s.refreshes) != 0 || !s.sessions[1].state.SelfStream {
				t.Fatalf("newer sync lost or stale state committed: reads=%d stream=%v pending=%d", reads.Load(), s.sessions[1].state.SelfStream, len(s.refreshes))
			}
			if len(r.updates["1"].Sources) != 2 {
				t.Fatalf("newest stream grants=%+v", r.updates["1"])
			}
		})
	}
}

func TestRefreshDoesNotOutliveMemberRemovalOrReplacement(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "replacement"}[replacement], func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				close(entered)
				<-release
				return permissions.ViewChannel | permissions.Connect, nil
			})
			s.RefreshGuild(7)
			go func() { s.refreshDue(t.Context()); close(done) }()
			waitRefreshSignal(t, entered)
			s.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
			if replacement {
				s.mu.Lock()
				s.sessions[1] = &session{room: "45", sid: "PA_new", state: State{GuildID: 8, ChannelID: 45, UserID: 1, SelfStream: true}}
				s.mu.Unlock()
			}
			unblock()
			waitRefreshSignal(t, done)
			if len(r.updates) != 0 || len(r.removed) != 0 {
				t.Fatal("stale permission read reached LiveKit")
			}
			if replacement && !s.sessions[1].state.SelfStream {
				t.Fatal("stale refresh changed a replacement session")
			}
		})
	}
}

func TestRefreshRPCDoesNotCommitToAReplacement(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		return permissions.ViewChannel | permissions.Connect, nil
	})
	bus := events.NewBus(nil)
	s.pub = bus
	frames, unsubscribe := bus.Subscribe(8)
	defer unsubscribe()
	r.before = func(string) error { close(entered); <-release; return nil }
	s.RefreshGuild(7)
	go func() { s.refreshDue(t.Context()); close(done) }()
	waitRefreshSignal(t, entered)
	s.mu.Lock()
	replacement := &session{room: "45", sid: "PA_new", state: State{GuildID: 8, ChannelID: 45, UserID: 1, SelfStream: true}}
	s.sessions[1] = replacement
	s.mu.Unlock()
	unblock()
	waitRefreshSignal(t, done)
	if !replacement.state.SelfStream {
		t.Fatal("old RPC cleared replacement stream state")
	}
	select {
	case ev := <-frames:
		t.Fatalf("stale RPC announced old-session state: %+v", ev)
	default:
	}
}

func TestRefreshSkipsObsoleteWorkBeforePermissionRead(t *testing.T) {
	for _, replacedSession := range []bool{false, true} {
		t.Run(map[bool]string{false: "new_sync", true: "new_session"}[replacedSession], func(t *testing.T) {
			reads := 0
			s, _ := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				reads++
				return permissions.ViewChannel | permissions.Connect, nil
			})
			s.RefreshGuild(7)
			job, captured := s.refreshes[7], s.sessions[1]
			if replacedSession {
				s.sessions[1] = &session{state: State{GuildID: 8, ChannelID: 45, UserID: 1}}
			} else {
				s.RefreshGuild(7)
			}
			if err := s.refreshParticipant(t.Context(), 7, 1, job, captured); err != nil || reads != 0 {
				t.Fatalf("obsolete refresh reads=%d error=%v, want no work", reads, err)
			}
		})
	}
}
