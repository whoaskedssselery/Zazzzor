package voice_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// moderationChannels supplies independent actor and target permissions.
type moderationChannels struct {
	actor, target permissions.Permission
	afterRead     func(snowflake.ID)
	subjects      map[snowflake.ID]permissions.Permission
}

func (*moderationChannels) Lookup(_ context.Context, id snowflake.ID) (channels.Channel, error) {
	guild := snowflake.ID(7)
	if id == 43 {
		guild = 8
	}
	return channels.Channel{ID: id, GuildID: guild, Type: channels.TypeVoice}, nil
}
func (c *moderationChannels) Permissions(ctx context.Context, user, channel snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	ch, _ := c.Lookup(ctx, channel)
	mask := c.target
	if user == 2 {
		mask = c.actor
	}
	if override, ok := c.subjects[user]; ok {
		mask = override
	}
	if c.afterRead != nil {
		c.afterRead(user)
	}
	if !mask.Has(permissions.ViewChannel) {
		return 0, 0, channels.ErrNotFound
	}
	return mask, ch.GuildID, nil
}

// moderationRooms records media updates and can block or fail one RPC.
type moderationRooms struct {
	*fakeRooms
	updates []voice.ParticipantPermissions
	before  func(voice.ParticipantPermissions) error
}

func (r *moderationRooms) UpdateParticipant(_ context.Context, _, _ string, p voice.ParticipantPermissions) error {
	if r.before != nil {
		if err := r.before(p); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, p)
	return nil
}
func (r *moderationRooms) last(t *testing.T) voice.ParticipantPermissions {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.updates) == 0 {
		t.Fatal("permission updates = none, want a LiveKit call")
	}
	return r.updates[len(r.updates)-1]
}

func moderationEnv(t *testing.T, actor permissions.Permission) (*env, *moderationChannels, *moderationRooms) {
	t.Helper()
	c := &moderationChannels{actor: permissions.ViewChannel | actor,
		target: permissions.ViewChannel | permissions.Connect | permissions.Speak | permissions.Stream}
	r := &moderationRooms{fakeRooms: newFakeRooms()}
	e := webhookEnv(t, c, func(o *voice.Options) { o.Rooms = r })
	e.rooms = r.fakeRooms
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_1", "initial")})
	return e, c, r
}

func TestModerationChecksEachPermissionBeforeUpdating(t *testing.T) {
	on := true
	for _, tc := range []struct {
		name    string
		mask    permissions.Permission
		f       voice.ModerationFields
		allowed bool
	}{
		{"mute", permissions.MuteMembers, voice.ModerationFields{Mute: &on}, true},
		{"deaf", permissions.DeafenMembers, voice.ModerationFields{Deaf: &on}, true},
		{"mute_is_not_deaf", permissions.MuteMembers, voice.ModerationFields{Deaf: &on}, false},
		{"deaf_is_not_mute", permissions.DeafenMembers, voice.ModerationFields{Mute: &on}, false},
		{"combined_requires_both", permissions.MuteMembers, voice.ModerationFields{Mute: &on, Deaf: &on}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, r := moderationEnv(t, tc.mask)
			before := len(r.updates)
			err := e.voice.Moderate(t.Context(), 7, 2, 1, tc.f)
			if tc.allowed {
				if err != nil {
					t.Fatalf("Moderate: %v", err)
				}
			} else {
				if !errors.Is(err, voice.ErrMissingPerm) {
					t.Fatalf("Moderate error = %v, want missing permission", err)
				}
				if len(r.updates) != before {
					t.Fatal("unauthorized patch reached LiveKit")
				}
			}
		})
	}
}

func TestModerationSurvivesRejoinAndRestrictsOldTokens(t *testing.T) {
	e, c, r := moderationEnv(t, permissions.MuteMembers|permissions.DeafenMembers)
	user := auth.User{ID: 1, DisplayName: "target"}
	old, err := e.voice.Join(t.Context(), 42, user, "https://example.test")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	on := true
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on, Deaf: &on}); err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_1", "")})
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_old_token", claims(t, old.Token).Attributes["zazzzor.join"])})
	st, _ := e.stateOf(7, 1)
	p := r.last(t)
	if !st.Mute || !st.Deaf || p.Subscribe || slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE) {
		t.Fatalf("rejoined state/grants = %+v/%+v, want muted and deaf", st, p)
	}
	if err = e.voice.Leave(t.Context(), 1); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	fresh, err := e.voice.Join(t.Context(), 42, user, "https://example.test")
	if err != nil {
		t.Fatalf("Join muted: %v", err)
	}
	g := claims(t, fresh.Token).Video
	if g.GetCanSubscribe() || slices.Contains(g.CanPublishSources, "microphone") {
		t.Fatalf("muted token grants = %+v", g)
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_fresh", claims(t, fresh.Token).Attributes["zazzzor.join"])})
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_fresh", "")})
	e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_reconciled", Join: claims(t, fresh.Token).Attributes["zazzzor.join"]})
	e.reconcile()
	st, _ = e.stateOf(7, 1)
	if p = r.last(t); !st.Mute || !st.Deaf || p.Subscribe || slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE) {
		t.Fatalf("reconciled state/grants=%+v/%+v, want muted and deaf", st, p)
	}
	c.target &^= permissions.Speak
	off := false
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &off, Deaf: &off}); err != nil {
		t.Fatalf("Unmute: %v", err)
	}
	if p = r.last(t); !p.Subscribe || slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE) {
		t.Fatalf("unmute grants = %+v, want listening without microphone", p)
	}
}

func TestMediaOperationsWaitForPendingModeration(t *testing.T) {
	for _, operation := range []string{"moderation", "token", "webhook", "reconcile"} {
		t.Run(operation, func(t *testing.T) {
			e, _, r := moderationEnv(t, permissions.MuteMembers)
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var calls atomic.Int32
			r.before = func(voice.ParticipantPermissions) error {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
				}
				return nil
			}
			on, off := true, false
			first := make(chan error, 1)
			go func() { first <- e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on}) }()
			awaitSignal(t, entered)
			done, started := make(chan error, 1), make(chan struct{})
			go func() {
				close(started)
				switch operation {
				case "moderation":
					done <- e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &off})
				case "token":
					_, err := e.voice.Join(t.Context(), 42, auth.User{ID: 1}, "https://example.test")
					done <- err
				case "webhook":
					done <- postJoined(e, "PA_ordered")
				case "reconcile":
					e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_1"})
					done <- e.voice.Reconcile(t.Context())
				}
			}()
			awaitSignal(t, started)
			var err error
			early := false
			select {
			case err = <-done:
				early = true
			case <-time.After(150 * time.Millisecond):
			}
			unblock()
			if firstErr := awaitError(t, first); firstErr != nil {
				t.Fatalf("first moderation: %v", firstErr)
			}
			if !early {
				err = awaitError(t, done)
			}
			if early || err != nil {
				t.Fatalf("%s completed during pending moderation=%v, error=%v", operation, early, err)
			}
		})
	}
}

func TestRemovalDuringMediaRPCDoesNotRestoreState(t *testing.T) {
	for _, operation := range []string{"moderation", "webhook"} {
		t.Run(operation, func(t *testing.T) {
			e, _, r := moderationEnv(t, permissions.MuteMembers)
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			r.before = func(voice.ParticipantPermissions) error { close(entered); <-release; return nil }
			done := make(chan error, 1)
			on := true
			go func() {
				if operation == "moderation" {
					done <- e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on})
				} else {
					done <- postJoined(e, "PA_during_remove")
				}
			}()
			awaitSignal(t, entered)
			e.voice.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
			unblock()
			err := awaitError(t, done)
			if operation == "moderation" && !errors.Is(err, channels.ErrNotFound) {
				t.Fatalf("Moderate error=%v, want not found", err)
			}
			if operation == "webhook" && err != nil {
				t.Fatalf("webhook: %v", err)
			}
			if states := e.voice.GuildStates(7); len(states) != 0 {
				t.Fatalf("state after removal=%v, want none", states)
			}
		})
	}
}

func postJoined(e *env, sid string) error {
	body, err := protojson.Marshal(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, sid, "")})
	if err != nil {
		return fmt.Errorf("encode joined webhook: %w", err)
	}
	resp := e.postHook(body, sign(e.t, testKey, testSecret, body))
	if resp.Status != http.StatusNoContent {
		return fmt.Errorf("joined webhook status=%d", resp.Status)
	}
	return nil
}

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(wait):
		t.Fatal("timed out waiting for operation barrier")
	}
}
func awaitError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(wait):
		t.Fatal("timed out waiting for operation result")
		return nil
	}
}
