package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func TestWebhookPanicReleasesDeliveryOwnership(t *testing.T) {
	t.Parallel()
	first := true
	s := NewService(Options{Rooms: &replacementRoom{}, APIKey: "key", APISecret: "secret",
		Channels: refreshChannels{read: func(context.Context, snowflake.ID) (permissions.Permission, error) {
			if first {
				first = false
				panic("simulated dependency panic")
			}
			return permissions.ViewChannel | permissions.Connect, nil
		}}})
	ev := &livekit.WebhookEvent{Id: "retry_after_panic", Event: eventJoined,
		Room: &livekit.Room{Name: "42", Sid: "RM_1"}, Participant: &livekit.ParticipantInfo{Identity: "1", Sid: "PA_new"}}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("dependency panic did not propagate")
			}
		}()
		_ = s.apply(t.Context(), ev)
	}()
	if len(s.inflight) != 0 || len(s.seen) != 0 {
		t.Fatalf("panic left inflight=%d seen=%d, want no delivery ownership or success", len(s.inflight), len(s.seen))
	}
	if err := s.apply(t.Context(), ev); err != nil || s.sessions[1] == nil {
		t.Fatalf("retry after panic: state=%+v error=%v", s.sessions[1], err)
	}
}

func TestWebhookLimitMessageUsesConfiguredByteLimit(t *testing.T) {
	t.Parallel()
	s := NewService(Options{Rooms: &replacementRoom{}, APIKey: "key", APISecret: "secret"})
	req := httptest.NewRequest(http.MethodPost, WebhookPath, bytes.NewReader(make([]byte, MaxWebhookBytes+1)))
	response := httptest.NewRecorder()
	httpx.Wrap(s.handleWebhook).ServeHTTP(response, req)
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode oversized-body problem: %v", err)
	}
	if want := fmt.Sprintf("request body exceeds %d bytes", MaxWebhookBytes); response.Code != http.StatusBadRequest || problem.Detail != want {
		t.Fatalf("oversized-body status=%d detail=%q, want 400/%q", response.Code, problem.Detail, want)
	}
}
