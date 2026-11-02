package voice_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// admissionChannels can pause or fail the permission read, without a database.
type admissionChannels struct {
	check func()
	fail  *atomic.Bool
}

func (a admissionChannels) Lookup(_ context.Context, id snowflake.ID) (channels.Channel, error) {
	return channels.Channel{ID: id, GuildID: 7, Type: channels.TypeVoice}, nil
}

func (a admissionChannels) Permissions(context.Context, snowflake.ID, snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	if a.check != nil {
		a.check()
	}
	if a.fail != nil && a.fail.Swap(false) {
		return 0, 0, errors.New("temporary permission read failure")
	}
	return permissions.ViewChannel | permissions.Connect, 7, nil
}

// webhookEnv reuses the signed-hook helpers with only in-memory dependencies.
func webhookEnv(t *testing.T, c voice.Channels, opts ...func(*voice.Options)) *env {
	t.Helper()
	e := &env{t: t, pub: &recorder{}, rooms: newFakeRooms()}
	o := voice.Options{Channels: c, Rooms: e.rooms, APIKey: testKey,
		APISecret: testSecret, Publisher: e.pub}
	for _, opt := range opts {
		opt(&o)
	}
	e.voice = voice.NewService(o)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { voice.MountWebhook(r, e.voice) })
	e.h = r
	e.lk = testkit.NewClient(t, r)
	e.lk.Origin = ""
	return e
}

func TestWebhookRetriesFailedAdmission(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	e := webhookEnv(t, admissionChannels{fail: &fail})
	ev := &livekit.WebhookEvent{Id: "retry", Event: "participant_joined", Room: lkRoom("42", "RM_1"),
		Participant: lkParticipant(userBody{ID: "1"}, "PA_1", "join")}
	body, err := protojson.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}
	authorization := sign(t, testKey, testSecret, body)
	expect(t, e.postHook(body, authorization), http.StatusInternalServerError)
	expect(t, e.postHook(body, authorization), http.StatusNoContent)
	if states := e.voice.GuildStates(7); len(states) != 1 {
		t.Fatalf("states after retry = %v, want one participant", states)
	}
}

func TestLateJoinedDoesNotReviveDepartedParticipant(t *testing.T) {
	e := webhookEnv(t, admissionChannels{})
	p := lkParticipant(userBody{ID: "1"}, "PA_left", "join")
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: p})
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("42", "RM_1"), Participant: p})
	if states := e.voice.GuildStates(7); len(states) != 0 {
		t.Fatalf("states = %v, want none after departure", states)
	}
}

func TestRemovalWhileJoinIsAdmittedDoesNotCreateState(t *testing.T) {
	for _, remove := range []string{"member", "channel"} {
		t.Run(remove, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			e := webhookEnv(t, admissionChannels{check: func() { close(entered); <-release }})
			ev := &livekit.WebhookEvent{Id: "inflight", Event: "participant_joined", Room: lkRoom("42", "RM_1"),
				Participant: lkParticipant(userBody{ID: "1"}, "PA_inflight", "join")}
			body, err := protojson.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal webhook: %v", err)
			}
			authorization := sign(t, testKey, testSecret, body)
			done := make(chan *testkit.Response, 1)
			go func() { done <- e.postHook(body, authorization) }()
			<-entered
			if remove == "member" {
				e.voice.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
			} else {
				e.voice.RemoveChannel(t.Context(), 42)
			}
			close(release)
			expect(t, <-done, http.StatusNoContent)
			if states := e.voice.GuildStates(7); len(states) != 0 {
				t.Fatalf("states after %s removal = %v, want none", remove, states)
			}
		})
	}
}

func TestReconcileDoesNotRestoreRemovedSnapshot(t *testing.T) {
	for _, removal := range []string{"member", "channel", "departure"} {
		t.Run(removal, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			e := webhookEnv(t, admissionChannels{check: func() { close(entered); <-release }})
			e.rooms.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_old"})
			done := make(chan error, 1)
			go func() { done <- e.voice.Reconcile(t.Context()) }()
			<-entered
			switch removal {
			case "member":
				e.voice.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
			case "channel":
				e.voice.RemoveChannel(t.Context(), 42)
			case "departure":
				e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_old", "")})
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if states := e.voice.GuildStates(7); len(states) != 0 {
				t.Fatalf("restored %s snapshot: %v", removal, states)
			}
		})
	}
}
