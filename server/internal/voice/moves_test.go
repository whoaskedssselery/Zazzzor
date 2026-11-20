package voice_test

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// destination asks for a move to the channel id.
func destination(id snowflake.ID) *voice.MoveRequest { return &voice.MoveRequest{ChannelID: id} }

// disconnect asks to take the participant out of voice.
func disconnect() *voice.MoveRequest { return &voice.MoveRequest{Disconnect: true} }

func TestVoiceMoveChecksBothChannelsBeforeEffects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src, dst permissions.Permission
		target   permissions.Permission
		id       snowflake.ID
		kind     channels.Type
		want     error
	}{
		{"source", 0, permissions.MoveMembers, permissions.Connect, 44, channels.TypeVoice, voice.ErrMissingPerm},
		{"destination", permissions.MoveMembers, 0, permissions.Connect, 44, channels.TypeVoice, voice.ErrMissingPerm},
		{"target_connect", permissions.MoveMembers, permissions.MoveMembers, 0, 44, channels.TypeVoice, voice.ErrMissingPerm},
		{"other_guild", permissions.MoveMembers, permissions.MoveMembers, permissions.Connect, 43, channels.TypeVoice, channels.ErrNotFound},
		{"text", permissions.MoveMembers, permissions.MoveMembers, permissions.Connect, 44, channels.TypeText, voice.ErrNotVoiceChannel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, c, r := moderationEnv(t, tc.src|permissions.MuteMembers)
			c.actorChannels = map[snowflake.ID]permissions.Permission{tc.id: permissions.ViewChannel | tc.dst}
			c.targetChannels = map[snowflake.ID]permissions.Permission{tc.id: permissions.ViewChannel | tc.target}
			c.kinds = map[snowflake.ID]channels.Type{tc.id: tc.kind}
			n, updates := len(e.pub.events), len(r.updates)
			on := true
			err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on, Move: destination(tc.id)})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Move error=%v, want %v", err, tc.want)
			}
			if len(e.pub.events) != n || len(r.updates) != updates || len(r.removals()) != 0 {
				t.Fatal("unauthorized move had partial effects")
			}
		})
	}
}

func TestVoiceMoveWaitsForDestinationAdmission(t *testing.T) {
	t.Parallel()
	e, _, r := moderationEnv(t, permissions.MoveMembers)
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	ev := e.pub.events[len(e.pub.events)-1]
	if ev.Type != events.VoiceMove || ev.Audience.Kind != events.AudienceUsers || !slices.Equal(ev.Audience.UserIDs, []snowflake.ID{1}) || ev.Audience.ChannelID != 44 {
		t.Fatalf("move audience=%+v, want target only", ev)
	}
	if payload, ok := ev.Payload.(voice.MovePayload); !ok || payload.ChannelID != 44 {
		t.Fatalf("move payload=%+v", ev.Payload)
	}
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("destination token: %v", err)
	}
	if st, _ := e.stateOf(7, 1); st.ChannelID != 42 || len(r.removals()) != 0 {
		t.Fatal("issuing destination token removed source early")
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_target"), Participant: lkParticipant(userBody{ID: "1"}, "PA_target", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 44 {
		t.Fatalf("moved state=%+v, want target", st)
	}
	e.rooms.put(voice.Room{Name: "44", SID: "RM_target"}, voice.Participant{Identity: "1", SID: "PA_target", Join: claims(t, j.Token).Attributes["zazzzor.join"]})
	e.run()
	e.rooms.waitRemoved(t, "42/1")
}

func TestVoiceMoveFallbackKeepsIssuedTargetToken(t *testing.T) {
	t.Parallel()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	e, _, r := moderationEnv(t, permissions.MoveMembers, func(o *voice.Options) { o.Now = func() time.Time { return time.Unix(0, clock.Load()) } })
	e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_1"})
	if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
	if err != nil {
		t.Fatalf("destination token: %v", err)
	}
	clock.Add(int64(14 * time.Second))
	e.reconcile()
	before := len(e.pub.events)
	if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
		t.Fatalf("repeat Move: %v", err)
	}
	if len(e.pub.events) != before {
		t.Fatal("identical move repeated its private event")
	}
	if st, _ := e.stateOf(7, 1); st.ChannelID != 42 || len(r.removals()) != 0 {
		t.Fatal("fallback fired before 15 seconds")
	}
	clock.Add(int64(time.Second))
	e.reconcile()
	if states := e.voice.GuildStates(7); len(states) != 0 {
		t.Fatalf("expired source states=%v, want none", states)
	}
	e.run()
	e.rooms.waitRemoved(t, "42/1")
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("44", "RM_target"), Participant: lkParticipant(userBody{ID: "1"}, "PA_late", claims(t, j.Token).Attributes["zazzzor.join"])})
	if st, _ := e.stateOf(7, 1); st.ChannelID != 44 {
		t.Fatalf("late target state=%+v, want target", st)
	}
}

func TestReconcilePreservesSourceUntilDestinationAdmission(t *testing.T) {
	t.Parallel()
	for _, obsolete := range []bool{true, false} {
		t.Run(map[bool]string{true: "obsolete_token", false: "failed_grants"}[obsolete], func(t *testing.T) {
			e, c, r := moderationEnv(t, permissions.MoveMembers)
			if err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)}); err != nil {
				t.Fatalf("Move: %v", err)
			}
			j, err := e.voice.Join(t.Context(), 44, auth.User{ID: 1}, "https://example.test")
			if err != nil {
				t.Fatalf("target token: %v", err)
			}
			if obsolete {
				if err = e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(45)}); err != nil {
					t.Fatalf("superseding Move: %v", err)
				}
			} else {
				c.targetChannels = map[snowflake.ID]permissions.Permission{44: permissions.ViewChannel | permissions.Connect}
				r.before = func(p voice.ParticipantPermissions) error {
					if len(p.Sources) == 0 {
						return voice.ErrUnavailable
					}
					return nil
				}
			}
			now := time.Now()
			e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_1", Join: "initial", JoinedAt: now.Add(-time.Second)})
			e.rooms.put(voice.Room{Name: "44", SID: "RM_target"}, voice.Participant{Identity: "1", SID: "PA_target", Join: claims(t, j.Token).Attributes["zazzzor.join"], JoinedAt: now})
			err = e.voice.Reconcile(t.Context())
			if !obsolete && !errors.Is(err, voice.ErrUnavailable) {
				t.Fatalf("failed destination grants: %v", err)
			}
			if st, ok := e.stateOf(7, 1); !ok || st.ChannelID != 42 {
				t.Fatalf("source lost before destination admission: %+v", st)
			}
			e.run()
			e.rooms.waitRemoved(t, "44/1")
			if slices.Contains(r.removals(), "42/1") {
				t.Fatal("pinned source queued for removal before deadline")
			}
		})
	}
}
