package voice_test

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestSuccessfulMediaRefreshInvalidatesOlderPermissionSnapshot(t *testing.T) {
	t.Parallel()
	for _, refresh := range []string{"moderation", "webhook", "reconcile"} {
		t.Run(refresh, func(t *testing.T) {
			e, c, r := moderationEnv(t, permissions.MuteMembers)
			e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_1"})
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var calls atomic.Int32
			c.afterRead = func(user snowflake.ID) {
				if user == 1 && calls.Add(1) == 1 {
					close(entered)
					<-release
				}
			}
			done := make(chan error, 1)
			go func() { done <- e.voice.Reconcile(t.Context()) }()
			awaitSignal(t, entered)
			c.target &^= permissions.Speak
			off := false
			var err error
			switch refresh {
			case "moderation":
				err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &off})
			case "webhook":
				err = postJoined(e, "PA_1")
			case "reconcile":
				err = e.voice.Reconcile(t.Context())
			}
			if err != nil {
				t.Fatalf("refresh permissions: %v", err)
			}
			unblock()
			if err = awaitError(t, done); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if p := r.last(t); slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE) {
				t.Fatalf("grants after older snapshot=%+v, want no microphone", p)
			}
		})
	}
}

func TestReconcileIsolatesFailedPermissionUpdate(t *testing.T) {
	t.Parallel()
	e, c, r := moderationEnv(t, permissions.MuteMembers|permissions.Connect)
	c.subjects = map[snowflake.ID]permissions.Permission{3: permissions.ViewChannel}
	room := voice.Room{Name: "42", SID: "RM_1"}
	// 1 is already connected, 2 and 4 are new, 3 may no longer connect.
	for _, p := range []voice.Participant{{Identity: "1", SID: "PA_1"}, {Identity: "2", SID: "PA_2"},
		{Identity: "3", SID: "PA_3"}, {Identity: "4", SID: "PA_4"}} {
		e.rooms.put(room, p)
	}
	r.before = func(p voice.ParticipantPermissions) error {
		if slices.Contains(p.Sources, livekit.TrackSource_MICROPHONE) {
			return errFake
		}
		return nil
	}
	if err := e.voice.Reconcile(t.Context()); !errors.Is(err, errFake) {
		t.Fatalf("Reconcile error=%v, want failed update cause", err)
	}
	if _, ok := e.stateOf(7, 4); ok {
		t.Fatal("failed admission kept public state")
	}
	if _, ok := e.stateOf(7, 1); !ok {
		t.Fatal("a failed update dropped a connected participant")
	}
	if _, ok := e.stateOf(7, 2); !ok {
		t.Fatal("one failed update prevented healthy admission")
	}
	e.run()
	e.rooms.waitRemoved(t, "42/4")
	e.rooms.waitRemoved(t, "42/3")
	if slices.Contains(e.rooms.removals(), "42/1") {
		t.Fatal("removed the connected participant whose grants failed")
	}
}

func TestFailedAdmissionRemovesParticipantInsteadOfPublishingState(t *testing.T) {
	t.Parallel()
	e, _, r := moderationEnv(t, permissions.MuteMembers)
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_1", "")})
	r.before = func(voice.ParticipantPermissions) error { return errFake }
	if err := postJoined(e, "PA_failed"); err == nil {
		t.Fatal("failed admission returned success")
	}
	if states := e.voice.GuildStates(7); len(states) != 0 {
		t.Fatalf("state after failed grants=%v, want none", states)
	}
	e.run()
	e.rooms.waitRemoved(t, "42/1")
}

func TestFailedModerationDoesNotPersistOrAnnounce(t *testing.T) {
	t.Parallel()
	e, _, r := moderationEnv(t, permissions.MuteMembers)
	r.before = func(voice.ParticipantPermissions) error { return errFake }
	n := len(e.pub.updates())
	on := true
	err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on})
	if !errors.Is(err, voice.ErrUnavailable) || !errors.Is(err, errFake) {
		t.Fatalf("Moderate error = %v, want upstream cause", err)
	}
	st, _ := e.stateOf(7, 1)
	if st.Mute || len(e.pub.updates()) != n {
		t.Fatal("failed moderation changed public state")
	}
}
