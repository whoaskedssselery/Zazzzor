package voice

import (
	"context"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// screenService supplies an owner and the same permission/RPC fakes as refresh tests.
func screenService(t *testing.T) (*Service, *refreshRooms, Room, Participant) {
	t.Helper()
	s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		return permissions.ViewChannel | permissions.Connect | permissions.Stream, nil
	})
	s.sessions[1].roomSID, s.sessions[1].join = "RM_1", "join"
	s.sessions[1].screens = map[string]bool{}
	s.sessions[1].state.SelfStream = false
	return s, r, Room{Name: "42", SID: "RM_1"}, Participant{Identity: "1.screen", SID: "SC_1", Join: "join"}
}

func TestScreenAdmissionAndAggregateTracks(t *testing.T) {
	t.Parallel()
	s, r, room, p := screenService(t)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("screen admission: %v", err)
	}
	if _, rejected := s.removals[removalKey{room.Name, p.Identity}]; rejected {
		t.Fatal("valid companion was rejected")
	}
	if grants, ok := r.updates[p.Identity]; !ok || grants.Subscribe || len(grants.Sources) != 2 {
		t.Fatalf("screen grants = %+v, want screen-only publisher", grants)
	}
	s.screen(t.Context(), p, "screen", true)
	if !s.sessions[1].state.SelfStream || len(s.sessions) != 1 {
		t.Fatal("screen publication did not update its owner")
	}
	main := Participant{Identity: "1", SID: "PA_1"}
	s.screen(t.Context(), main, "main", true)
	s.screen(t.Context(), p, "screen", false)
	if !s.sessions[1].state.SelfStream {
		t.Fatal("companion unpublication cleared the primary screen")
	}
	s.screen(t.Context(), main, "main", false)
	if s.sessions[1].state.SelfStream {
		t.Fatal("self_stream remained without a screen")
	}
	s.screen(t.Context(), p, "screen", true)
	s.left(t.Context(), p)
	if s.sessions[1] == nil || s.sessions[1].state.SelfStream {
		t.Fatal("screen departure removed owner or kept its screen")
	}
}

func TestScreenAdmissionRejectsInvalidBinding(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"owner", "room", "room SID", "generation", "stream", "departed"} {
		t.Run(reason, func(t *testing.T) {
			s, _, room, p := screenService(t)
			switch reason {
			case "owner":
				delete(s.sessions, 1)
			case "room":
				room.Name = "43"
			case "room SID":
				room.SID = "RM_old"
			case "generation":
				p.Join = "old"
			case "stream":
				s.channels = retryGrantChannels{}
			case "departed":
				s.left(t.Context(), p)
			}
			if err := s.joined(t.Context(), 42, room, p); err != nil {
				t.Fatalf("screen admission: %v", err)
			}
			if s.removals[removalKey{room.Name, p.Identity}] == nil {
				t.Fatal("invalid companion not queued for removal")
			}
		})
	}
}

func TestScreenRemovedWithPrimary(t *testing.T) {
	t.Parallel()
	for _, cause := range []string{"leave", "departure", "replacement", "finished", "user", "channel", "members", "move", "move expiry", "channel join", "disconnect"} {
		t.Run(cause, func(t *testing.T) {
			s, _, room, p := screenService(t)
			if err := s.joined(t.Context(), 42, room, p); err != nil {
				t.Fatalf("screen admission: %v", err)
			}
			delete(s.removals, removalKey{room.Name, p.Identity})
			switch cause {
			case "leave":
				if err := s.Leave(t.Context(), 1); err != nil {
					t.Fatalf("Leave: %v", err)
				}
			case "departure":
				s.left(t.Context(), Participant{Identity: "1", SID: "PA_1"})
			case "replacement":
				s.mu.Lock()
				s.connectLocked(t.Context(), 7, 42, room, Participant{Identity: "1", SID: "PA_2", Join: "new"})
				s.mu.Unlock()
			case "finished":
				s.finished(t.Context(), 42, room.SID)
			case "user":
				s.RemoveUser(t.Context(), 1)
			case "channel":
				s.RemoveChannel(t.Context(), 42)
			case "members":
				s.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
			case "move":
				s.mu.Lock()
				s.requestMoveLocked(t.Context(), 1, 43, s.sessions[1])
				s.connectLocked(t.Context(), 7, 43, Room{Name: "43", SID: "RM_2"}, Participant{Identity: "1", SID: "PA_2", Join: "new"})
				s.mu.Unlock()
			case "move expiry":
				s.mu.Lock()
				s.requestMoveLocked(t.Context(), 1, 43, s.sessions[1])
				s.moves[1].deadline = s.now().Add(-time.Second)
				s.expireMovesLocked(t.Context())
				s.mu.Unlock()
			case "channel join":
				if err := s.leaveOther(t.Context(), 1, 43); err != nil {
					t.Fatalf("leaveOther: %v", err)
				}
			case "disconnect":
				if err := s.disconnect(t.Context(), 7, 1, s.sessions[1], serverFlags{}, s.rev); err != nil {
					t.Fatalf("disconnect: %v", err)
				}
			}
			if s.removals[removalKey{room.Name, p.Identity}] == nil {
				t.Fatal("primary ended without removing companion")
			}
		})
	}
}

func TestScreenRepeatedAdmissionKeepsPublishedTracks(t *testing.T) {
	t.Parallel()
	s, _, room, p := screenService(t)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("admission: %v", err)
	}
	s.screen(t.Context(), p, "published", true)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("repeated admission: %v", err)
	}
	if !s.sessions[1].state.SelfStream {
		t.Fatal("repeated join erased a later track event")
	}
}

func TestScreenOldJoinCannotReplaceNewCompanion(t *testing.T) {
	t.Parallel()
	s, _, room, p := screenService(t)
	newerScreen := p
	newerScreen.SID = "SC_2"
	newerScreen.Screens = []string{"new"}
	if err := s.joined(t.Context(), 42, room, newerScreen); err != nil {
		t.Fatalf("new admission: %v", err)
	}
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("old admission: %v", err)
	}
	if s.sessions[1].companion.p.SID != newerScreen.SID || !s.sessions[1].state.SelfStream {
		t.Fatal("late join replaced the newer companion")
	}
}

func TestScreenIgnoresOldTrackAndDepartureEvents(t *testing.T) {
	t.Parallel()
	s, _, room, p := screenService(t)
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("admission: %v", err)
	}
	old := p
	old.SID = "SC_old"
	s.screen(t.Context(), old, "old", true)
	if s.sessions[1].state.SelfStream {
		t.Fatal("old track set self_stream")
	}
	s.left(t.Context(), old)
	s.screen(t.Context(), p, "current", true)
	if !s.sessions[1].state.SelfStream {
		t.Fatal("old departure cleared accepted companion")
	}
}

func TestScreenServerFlagsPreserveCompanion(t *testing.T) {
	t.Parallel()
	s, r, room, p := screenService(t)
	p.Screens = []string{"screen"}
	if err := s.joined(t.Context(), 42, room, p); err != nil {
		t.Fatalf("admission: %v", err)
	}
	s.moderation[memberKey{guildID: 7, userID: 1}] = serverFlags{mute: true, deaf: true}
	delete(r.updates, p.Identity)
	s.RefreshGuild(7)
	s.refreshDue(t.Context())
	if _, updated := r.updates[p.Identity]; updated || s.sessions[1].companion == nil || !s.sessions[1].state.SelfStream {
		t.Fatal("server mute/deaf changed companion grants or screen")
	}
}
