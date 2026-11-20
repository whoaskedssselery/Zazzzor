package voice_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/go-chi/chi/v5"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// orderingService uses only in-process dependencies, so every goroutine and
// channel belongs to the synctest bubble, with no HTTP server or socket I/O.
func orderingService(t *testing.T) (*voice.Service, *moderationChannels, *moderationRooms) {
	t.Helper()
	c := &moderationChannels{actor: permissions.All, target: permissions.All}
	r := &moderationRooms{fakeRooms: newFakeRooms()}
	r.put(voice.Room{Name: "42", SID: "RM_1"}, voice.Participant{Identity: "1", SID: "PA_1"})
	s := voice.NewService(voice.Options{Channels: c, Members: hierarchyMembers{1: hierarchySubject(1, 9, 0), 2: hierarchySubject(2, 9, 1)}, Rooms: r, APIKey: testKey, APISecret: testSecret, Publisher: &recorder{}})
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	return s, c, r
}

// directOrderedHook exercises the real webhook route without a TLS test client.
func directOrderedHook(t *testing.T, s *voice.Service) error {
	t.Helper()
	body, err := protojson.Marshal(&livekit.WebhookEvent{Event: "participant_joined", Id: "EV_order", Room: lkRoom("42", "RM_1"), Participant: lkParticipant(userBody{ID: "1"}, "PA_ordered", "")})
	if err != nil {
		return fmt.Errorf("encode ordering webhook: %w", err)
	}
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) { voice.MountWebhook(r, s) })
	req := httptest.NewRequest(http.MethodPost, "/api/v1"+voice.WebhookPath, bytes.NewReader(body))
	req.Header.Set("Authorization", sign(t, testKey, testSecret, body))
	req.Header.Set("Content-Type", "application/webhook+json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		return fmt.Errorf("ordering webhook status = %d, want204", response.Code)
	}
	return nil
}

func TestMediaOperationsWaitForPendingModeration(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"moderation", "token", "webhook", "reconcile"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, c, r := orderingService(t)
				entered, release := make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				defer unblock()
				var orderMu sync.Mutex
				var order []string
				record := func(event string) { orderMu.Lock(); defer orderMu.Unlock(); order = append(order, event) }
				var calls atomic.Int32
				r.before = func(voice.ParticipantPermissions) error {
					if calls.Add(1) == 1 {
						close(entered)
						<-release
						record("first RPC released")
					} else if operation != "reconcile" {
						record("second effect")
					}
					return nil
				}
				on, off := true, false
				first := make(chan error, 1)
				go func() { first <- s.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on}) }()
				<-entered
				if operation == "token" {
					c.afterRead = func(snowflake.ID) { record("second effect") }
				}
				done := make(chan error, 1)
				go func() {
					var err error
					switch operation {
					case "moderation":
						err = s.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &off})
					case "token":
						_, err = s.Join(t.Context(), 42, auth.User{ID: 1}, "https://example.test")
					case "webhook":
						err = directOrderedHook(t, s)
					case "reconcile":
						err = s.Reconcile(t.Context())
						record("second effect")
					}
					done <- err
				}()
				// Wait runs the contender until it is durably blocked on the stripe.
				// Without the guard it finishes or reaches its effect before we release.
				synctest.Wait()
				unblock()
				if err := <-first; err != nil {
					t.Fatalf("first Moderate: %v", err)
				}
				if err := <-done; err != nil {
					t.Fatalf("%s: %v", operation, err)
				}
				if !slices.Equal(order, []string{"first RPC released", "second effect"}) {
					t.Fatalf("effect order = %v, want release before contender effect", order)
				}
			})
		})
	}
}

func TestCanceledStripeAcquisitionReturnsBeforeOwnerReleases(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, r := orderingService(t)
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		r.before = func(voice.ParticipantPermissions) error { close(entered); <-release; return nil }
		on := true
		first := make(chan error, 1)
		go func() { first <- s.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on}) }()
		<-entered
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := s.Join(ctx, 42, auth.User{ID: 1}, "https://example.test"); done <- err }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Join error = %v, want canceled", err)
			}
		default:
			t.Error("canceled Join remained blocked on the owner")
		}
		unblock()
		if err := <-first; err != nil {
			t.Fatalf("first Moderate: %v", err)
		}
	})
}
