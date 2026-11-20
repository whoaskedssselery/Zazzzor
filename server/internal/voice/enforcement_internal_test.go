package voice

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type refreshChannels struct {
	retryGrantChannels
	read func(context.Context, snowflake.ID) (permissions.Permission, error)
}

func (c refreshChannels) Permissions(ctx context.Context, user, _ snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	p, err := c.read(ctx, user)
	return p, 7, err
}

type refreshRooms struct {
	replacementRoom
	updates map[string]ParticipantPermissions
	removed map[string]bool
	before  func(string) error
}

func (r *refreshRooms) UpdateParticipant(_ context.Context, _, user string, p ParticipantPermissions) error {
	if r.before != nil {
		if err := r.before(user); err != nil {
			return err
		}
	}
	r.updates[user] = p
	return nil
}

func (r *refreshRooms) RemoveParticipant(_ context.Context, _, user string) error {
	if r.before != nil {
		if err := r.before(user); err != nil {
			return err
		}
	}
	r.removed[user] = true
	return nil
}

// refreshService supplies one streaming participant without a remote listing.
func refreshService(t *testing.T, read func(context.Context, snowflake.ID) (permissions.Permission, error)) (*Service, *refreshRooms) {
	t.Helper()
	r := &refreshRooms{updates: map[string]ParticipantPermissions{}, removed: map[string]bool{}}
	s := NewService(Options{Channels: refreshChannels{read: read}, Rooms: r, APIKey: "key", APISecret: "secret"})
	s.sessions[1] = &session{room: "42", sid: "PA_1", screens: map[string]bool{"SC_1": true},
		cameras: map[string]bool{"CA_1": true},
		state:   State{GuildID: 7, ChannelID: 42, UserID: 1, SelfStream: true, SelfVideo: true}}
	return s, r
}

func TestRefreshAppliesEachPermissionLoss(t *testing.T) {
	t.Parallel()
	all := permissions.ViewChannel | permissions.Connect | permissions.Speak | permissions.Stream
	for _, tc := range []struct {
		name string
		lost permissions.Permission
	}{
		{"view", permissions.ViewChannel}, {"connect", permissions.Connect},
		{"speak", permissions.Speak}, {"stream", permissions.Stream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
				if tc.lost == permissions.ViewChannel {
					return 0, channels.ErrNotFound
				}
				return all &^ tc.lost, nil
			})
			s.RefreshGuild(7)
			s.refreshDue(t.Context())
			if tc.lost == permissions.ViewChannel || tc.lost == permissions.Connect {
				if !r.removed["1"] || s.sessions[1] != nil {
					t.Fatal("lost admission did not remove participant/state")
				}
				return
			}
			p, ok := r.updates["1"]
			if !ok || !p.Subscribe {
				t.Fatalf("updated grants=%+v, want subscribed", p)
			}
			if mic := slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE); mic != (tc.lost != permissions.Speak) {
				t.Fatalf("microphone=%v after loss %s", mic, tc.name)
			}
			if stream := slices.Contains(p.Sources, livekit.TrackSource_SCREEN_SHARE); stream != (tc.lost != permissions.Stream) {
				t.Fatalf("screen=%v after loss %s", stream, tc.name)
			}
			if camera := slices.Contains(p.Sources, livekit.TrackSource_CAMERA); camera != (tc.lost != permissions.Stream) {
				t.Fatalf("camera=%v after loss %s", camera, tc.name)
			}
			if tc.lost == permissions.Stream && (s.sessions[1].state.SelfStream || len(s.sessions[1].screens) != 0 ||
				s.sessions[1].state.SelfVideo || len(s.sessions[1].cameras) != 0) {
				t.Fatal("successfully revoked stream or camera remained public")
			}
		})
	}
}

func TestRefreshPreservesServerModeration(t *testing.T) {
	t.Parallel()
	s, r := refreshService(t, func(context.Context, snowflake.ID) (permissions.Permission, error) {
		return permissions.ViewChannel | permissions.Connect | permissions.Speak | permissions.Stream, nil
	})
	s.moderation[memberKey{guildID: 7, userID: 1}] = serverFlags{mute: true, deaf: true}
	s.RefreshGuild(7)
	s.refreshDue(t.Context())
	if p := r.updates["1"]; p.Subscribe || slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE) || !slices.Contains(p.Sources, livekit.TrackSource_SCREEN_SHARE) {
		t.Fatalf("moderated grants=%+v, want deaf/muted with independent screen", p)
	}
}

func TestRefreshRetriesFailuresWithoutBlockingOtherUsers(t *testing.T) {
	t.Parallel()
	for _, database := range []bool{false, true} {
		t.Run(map[bool]string{true: "database", false: "livekit"}[database], func(t *testing.T) {
			failed := true
			s, r := refreshService(t, func(_ context.Context, user snowflake.ID) (permissions.Permission, error) {
				if database && failed && user == 1 {
					return 0, errors.New("temporary database failure")
				}
				return permissions.ViewChannel | permissions.Connect, nil
			})
			if !database {
				r.before = func(user string) error {
					if failed && user == "1" {
						return errors.New("temporary media failure")
					}
					return nil
				}
			}
			s.sessions[2] = &session{room: "42", sid: "PA_2", state: State{GuildID: 7, ChannelID: 42, UserID: 2}}
			now := time.Now()
			s.now = func() time.Time { return now }
			s.RefreshGuild(7)
			if _, pending := s.refreshDue(t.Context()); !pending {
				t.Fatal("failed refresh was not queued for retry")
			}
			if _, ok := r.updates["2"]; !ok || !s.sessions[1].state.SelfStream {
				t.Fatal("failure blocked healthy user or announced unsuccessful change")
			}
			failed, now = false, now.Add(time.Minute)
			s.refreshDue(t.Context())
			if s.sessions[1].state.SelfStream || len(s.refreshes) != 0 {
				t.Fatal("recovered refresh did not converge")
			}
		})
	}
}
