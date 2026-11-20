package voice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type reconcileRooms struct {
	replacementRoom
	rooms        []Room
	participants map[string][]Participant
	listErrors   map[string]error
	roomsError   error
}

func (r *reconcileRooms) ListRooms(context.Context) ([]Room, error) {
	return r.rooms, r.roomsError
}

func (r *reconcileRooms) ListParticipants(_ context.Context, room string) ([]Participant, error) {
	return r.participants[room], r.listErrors[room]
}

type partialChannels struct {
	retryGrantChannels
	lookupFailure bool
	failure       error
}

func (c partialChannels) Lookup(ctx context.Context, channel snowflake.ID) (channels.Channel, error) {
	if c.lookupFailure && channel == 42 {
		return channels.Channel{}, c.failure
	}
	return c.retryGrantChannels.Lookup(ctx, channel)
}

func (c partialChannels) Permissions(_ context.Context, user, channel snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	if channel == 42 && user == 3 {
		return 0, 0, c.failure
	}
	if user == 5 {
		return 0, 0, channels.ErrNotFound
	}
	return permissions.ViewChannel | permissions.Connect | permissions.Speak, 7, nil
}

func TestReconcileIsolatesRoomPermissionFailures(t *testing.T) {
	t.Parallel()
	for _, lookupFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "permissions", true: "lookup"}[lookupFailure], func(t *testing.T) {
			failure := errors.New("temporary database failure")
			r := &reconcileRooms{
				rooms: []Room{{Name: "42", SID: "RM_bad"}, {Name: "43", SID: "RM_good"}},
				participants: map[string][]Participant{
					"42": {{Identity: "1", SID: "PA_candidate"}, {Identity: "5", SID: "PA_refused"}, {Identity: "3", SID: "PA_bad"}},
					"43": {{Identity: "2", SID: "PA_good"}},
				},
			}
			s := NewService(Options{Channels: partialChannels{lookupFailure: lookupFailure, failure: failure}, Rooms: r, APIKey: "key", APISecret: "secret"})
			old := &session{sid: "PA_old", room: "42", state: State{GuildID: 7, ChannelID: 42, UserID: 1}}
			unlisted := &session{sid: "PA_unlisted", room: "42", state: State{GuildID: 7, ChannelID: 42, UserID: 4}}
			s.sessions[1], s.sessions[4] = old, unlisted
			err := s.Reconcile(t.Context())
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), "42") {
				t.Fatalf("reconcile error=%v, want contextual DB cause", err)
			}
			if healthy := s.sessions[2]; healthy == nil || healthy.sid != "PA_good" {
				t.Fatal("one room's DB failure prevented healthy room recovery")
			}
			if s.sessions[1] != old || s.sessions[4] != unlisted || len(s.removals) != 0 {
				t.Fatal("failed room was partially applied or its old states removed")
			}
		})
	}
}

func TestReconcileReportsPartialListingFailures(t *testing.T) {
	t.Parallel()
	failure := errors.New("temporary room listing failure")
	r := &reconcileRooms{
		rooms:        []Room{{Name: "42"}, {Name: "43"}},
		participants: map[string][]Participant{"43": {{Identity: "2", SID: "PA_good"}}},
		listErrors:   map[string]error{"42": failure},
	}
	s := NewService(Options{Channels: retryGrantChannels{}, Rooms: r, APIKey: "key", APISecret: "secret"})
	old := &session{sid: "PA_old", room: "42", state: State{GuildID: 7, ChannelID: 42, UserID: 1}}
	s.sessions[1] = old
	if err := s.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("partial list error=%v, want original cause", err)
	}
	if s.sessions[1] != old || s.sessions[2] == nil {
		t.Fatal("partial listing discarded old state or skipped healthy room")
	}
}

func TestReconcileWrapsRoomListFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("room API unavailable")
	s := NewService(Options{Rooms: &reconcileRooms{roomsError: failure}, APIKey: "key", APISecret: "secret"})
	if err := s.Reconcile(t.Context()); !errors.Is(err, failure) || !strings.Contains(err.Error(), "voice: reconcile: list rooms") {
		t.Fatalf("list rooms error=%v, want contextual cause", err)
	}
}

func TestReconcileDoesNotReplaceAnUnknownRoomFromAnotherRoom(t *testing.T) {
	t.Parallel()
	for _, listingFailure := range []bool{false, true} {
		for _, reversed := range []bool{false, true} {
			name := map[bool]string{false: "database", true: "listing"}[listingFailure] + map[bool]string{false: "/first", true: "/last"}[reversed]
			t.Run(name, func(t *testing.T) {
				failure := errors.New("unknown source room")
				now := time.Now()
				r := &reconcileRooms{rooms: []Room{{Name: "42", SID: "RM_new"}, {Name: "43", SID: "RM_old"}},
					participants: map[string][]Participant{
						"42": {{Identity: "1", SID: "PA_new", JoinedAt: now}, {Identity: "3", SID: "PA_error"}},
						"43": {{Identity: "1", SID: "PA_old", JoinedAt: now.Add(-time.Minute)}, {Identity: "2", SID: "PA_healthy"}},
					}}
				if listingFailure {
					r.listErrors = map[string]error{"42": failure}
				}
				if reversed {
					r.rooms[0], r.rooms[1] = r.rooms[1], r.rooms[0]
				}
				s := NewService(Options{Channels: partialChannels{failure: failure}, Rooms: r, APIKey: "key", APISecret: "secret"})
				current := &session{sid: "PA_new", room: "42", state: State{GuildID: 7, ChannelID: 42, UserID: 1, JoinedAt: now}}
				s.sessions[1] = current // Recovered after restart, with no stored intent.
				if err := s.Reconcile(t.Context()); !errors.Is(err, failure) {
					t.Fatalf("partial reconcile error=%v, want original cause", err)
				}
				if s.sessions[1] != current || s.sessions[2] == nil {
					t.Fatal("cross-room duplicate replaced unknown-room state or blocked independent recovery")
				}
				if s.removals[removalKey{room: "42", identity: "1"}] != nil {
					t.Fatal("unknown-room session was queued for removal")
				}
			})
		}
	}
}
