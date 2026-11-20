package voice

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// scriptedRemovalRoom answers each removal with remove.
type scriptedRemovalRoom struct {
	replacementRoom
	remove func() error
}

func (r *scriptedRemovalRoom) RemoveParticipant(context.Context, string, string) error {
	return r.remove()
}

func TestRemovalReplacementInheritsFailedAttempt(t *testing.T) {
	t.Parallel()
	now := time.Now()
	var s *Service
	calls := 0
	r := &scriptedRemovalRoom{remove: func() error {
		calls++
		s.mu.Lock()
		s.removeLocked("42", "1", "PA_old")
		s.mu.Unlock()
		return errors.New("media unavailable")
	}}
	s = NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret", Now: func() time.Time { return now }})
	s.removeLocked("42", "1", "PA_new")
	next, pending := s.removeDue(t.Context())
	job := s.removals[removalKey{room: "42", identity: "1"}]
	if calls != 1 || !pending || !next.Equal(now.Add(time.Second)) || job.attempts != 1 {
		t.Fatalf("calls = %d, pending = %v, next = %v, job = %+v, want one failed attempt and one second backoff", calls, pending, next, job)
	}
}

func TestRemovalRetriesPastReconcileUntilRecovery(t *testing.T) {
	t.Parallel()
	now := time.Now()
	failed, calls := true, 0
	r := &scriptedRemovalRoom{remove: func() error {
		calls++
		if failed {
			return errors.New("media unavailable")
		}
		return nil
	}}
	s := NewService(Options{Rooms: r, APIKey: "key", APISecret: "secret", Now: func() time.Time { return now }})
	s.removeLocked("42", "1", "PA_new")
	for attempt := 1; attempt <= 100; attempt++ {
		next, pending := s.removeDue(t.Context())
		if !pending || !next.After(now) || next.After(now.Add(time.Minute)) {
			t.Fatalf("attempt %d next = %v, pending = %v, want positive backoff capped at one minute", attempt, next, pending)
		}
		now = next
	}
	failed = false
	s.removeDue(t.Context())
	if calls != 101 || len(s.removals) != 0 {
		t.Fatalf("calls = %d, pending = %d, want 101 and 0 after recovery", calls, len(s.removals))
	}
}

func TestRefreshRetriesOnlyFailedUsersAndNewSyncCoversAll(t *testing.T) {
	t.Parallel()
	now := time.Now()
	reads := map[snowflake.ID]int{}
	s, r := refreshService(t, func(_ context.Context, user snowflake.ID) (permissions.Permission, error) {
		reads[user]++
		return permissions.ViewChannel | permissions.Connect, nil
	})
	s.now = func() time.Time { return now }
	s.sessions[2] = &session{room: "42", sid: "PA_2", state: State{GuildID: 7, ChannelID: 42, UserID: 2}}
	r.before = func(user string) error {
		if user == "1" {
			return errors.New("media unavailable")
		}
		return nil
	}
	s.RefreshGuild(7)
	s.refreshDue(t.Context())
	now = now.Add(time.Minute)
	s.refreshDue(t.Context())
	if reads[1] != 2 || reads[2] != 1 {
		t.Fatalf("reads = %v, want failed user twice and successful user once", reads)
	}
	s.RefreshGuild(7)
	r.before = nil
	s.refreshDue(t.Context())
	if reads[1] != 3 || reads[2] != 2 || len(s.refreshes) != 0 {
		t.Fatalf("new sync reads = %v, pending = %d, want all refreshed and no pending work", reads, len(s.refreshes))
	}
}

func TestRefreshSupersededPermissionReadSkipsRemoteEffect(t *testing.T) {
	t.Parallel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		close(entered)
		<-release
		return 0, nil
	})
	s.RefreshGuild(7)
	job, captured := s.refreshes[7], s.sessions[1]
	go func() {
		defer close(done)
		if err := s.refreshParticipant(t.Context(), 7, 1, job, captured); err != nil {
			t.Errorf("refreshParticipant: %v", err)
		}
	}()
	waitRefreshSignal(t, entered)
	s.RefreshGuild(7)
	unblock()
	waitRefreshSignal(t, done)
	if len(r.removed) != 0 || len(r.updates) != 0 || s.sessions[1] != captured {
		t.Fatalf("removed = %v, updates = %v, retained = %v, want no stale remote effects", r.removed, r.updates, s.sessions[1] == captured)
	}
}

func TestRefreshQueueCoalescesRepeatedSyncs(t *testing.T) {
	t.Parallel()
	s, _ := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		return permissions.ViewChannel | permissions.Connect, nil
	})
	for range 1000 {
		s.RefreshGuild(7)
		s.RefreshGuild(8)
	}
	if len(s.refreshes) != 1 || len(s.refreshWake) != 1 {
		t.Fatalf("queued guilds = %d, wakeups = %d, want one active guild and one wakeup", len(s.refreshes), len(s.refreshWake))
	}
	s.refreshDue(t.Context())
	if len(s.refreshes) != 0 {
		t.Fatalf("pending guilds = %d, want 0", len(s.refreshes))
	}
}

// lockCheckingLog records whether s.mu was held when a line was logged.
type lockCheckingLog struct {
	s      *Service
	calls  int
	locked bool
}

func (*lockCheckingLog) Enabled(context.Context, slog.Level) bool { return true }

func (h *lockCheckingLog) Handle(context.Context, slog.Record) error {
	h.calls++
	if h.s.mu.TryLock() {
		h.s.mu.Unlock()
	} else {
		h.locked = true
	}
	return nil
}

func (h *lockCheckingLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *lockCheckingLog) WithGroup(string) slog.Handler      { return h }

func TestWorkerWarningsReleaseStateLock(t *testing.T) {
	t.Parallel()
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "removal", true: "refresh"}[refresh], func(t *testing.T) {
			now := time.Unix(1_700_000_000, 0)
			var s *Service
			if refresh {
				s, _ = refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
					return 0, errors.New("database unavailable")
				})
			} else {
				s = NewService(Options{Rooms: &failingRemovalRoom{}, APIKey: "key", APISecret: "secret"})
			}
			s.now = func() time.Time { return now }
			if refresh {
				s.RefreshGuild(7)
			} else {
				s.removeLocked("42", "1", "PA_new")
			}
			handler := &lockCheckingLog{s: s}
			s.log = slog.New(handler)
			for range 10 {
				if refresh {
					s.refreshDue(t.Context())
				} else {
					s.removeDue(t.Context())
				}
				now = now.Add(time.Minute)
			}
			want := 1
			if refresh {
				want = 10
			}
			if handler.locked || handler.calls != want {
				t.Fatalf("logged under lock = %v, warnings = %d, want false and %d", handler.locked, handler.calls, want)
			}
		})
	}
}

// scriptedMoveChannels answers permission reads with read.
type scriptedMoveChannels struct {
	retryGrantChannels
	read func(context.Context, snowflake.ID) (permissions.Permission, snowflake.ID, error)
}

func (c scriptedMoveChannels) Permissions(ctx context.Context, _ snowflake.ID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	return c.read(ctx, channelID)
}

func TestRefreshCancelsMoveWhenDestinationPermissionLost(t *testing.T) {
	t.Parallel()
	for _, lost := range []permissions.Permission{permissions.ViewChannel, permissions.Connect} {
		t.Run(map[permissions.Permission]string{permissions.ViewChannel: "view", permissions.Connect: "connect"}[lost], func(t *testing.T) {
			now := time.Now()
			s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				return permissions.ViewChannel | permissions.Connect, nil
			})
			s.now = func() time.Time { return now }
			s.channels = scriptedMoveChannels{read: func(_ context.Context, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error) {
				p := permissions.ViewChannel | permissions.Connect
				if channelID == 44 {
					p &^= lost
				}
				return p, 7, nil
			}}
			source := s.sessions[1]
			s.requestMoveLocked(t.Context(), 1, 44, source)
			s.RefreshGuild(7)
			s.refreshDue(t.Context())
			now = now.Add(20 * time.Second)
			s.removeDue(t.Context())
			if s.moves[1] != nil || s.sessions[1] != source || len(r.removed) != 0 {
				t.Fatalf("move = %+v, source retained = %v, removals = %v, want cancelled move and retained source", s.moves[1], s.sessions[1] == source, r.removed)
			}
		})
	}
}

func TestMoveRefreshRejectsSupersededDestinationRead(t *testing.T) {
	t.Parallel()
	for _, replace := range []string{"new_sync", "new_session", "new_move"} {
		t.Run(replace, func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			s, _ := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				return permissions.ViewChannel | permissions.Connect, nil
			})
			s.channels = scriptedMoveChannels{read: func(_ context.Context, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error) {
				if channelID == 44 {
					close(entered)
					<-release
					return 0, 7, nil
				}
				return permissions.ViewChannel | permissions.Connect, 7, nil
			}}
			source := s.sessions[1]
			s.requestMoveLocked(t.Context(), 1, 44, source)
			s.RefreshGuild(7)
			job := s.refreshes[7]
			go func() {
				defer close(done)
				if err := s.refreshParticipant(t.Context(), 7, 1, job, source); err != nil {
					t.Errorf("refreshParticipant: %v", err)
				}
			}()
			waitRefreshSignal(t, entered)
			s.mu.Lock()
			switch replace {
			case "new_sync":
				s.queueRefreshLocked(7)
			case "new_session":
				s.sessions[1] = &session{room: "42", sid: "PA_replacement", state: source.state}
			case "new_move":
				s.moves[1] = &pendingMove{source: source, channelID: 45, deadline: time.Now().Add(time.Minute)}
			}
			expected := s.moves[1]
			s.mu.Unlock()
			unblock()
			waitRefreshSignal(t, done)
			if s.moves[1] != expected {
				t.Fatalf("move = %+v, want current pending move %+v after stale read", s.moves[1], expected)
			}
		})
	}
}
