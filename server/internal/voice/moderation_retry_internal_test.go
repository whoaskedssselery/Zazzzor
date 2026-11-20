package voice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type retryGrantChannels struct{}

func (retryGrantChannels) Lookup(_ context.Context, id snowflake.ID) (channels.Channel, error) {
	return channels.Channel{ID: id, GuildID: 7, Type: channels.TypeVoice}, nil
}
func (retryGrantChannels) Permissions(context.Context, snowflake.ID, snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	return permissions.ViewChannel | permissions.Connect | permissions.Speak, 7, nil
}

type retryGrantRoom struct {
	replacementRoom
	failed          bool
	listed, release chan struct{}
}

func (r *retryGrantRoom) UpdateParticipant(context.Context, string, string, ParticipantPermissions) error {
	if !r.failed {
		r.failed = true
		return errors.New("temporary permission failure")
	}
	return nil
}
func (r *retryGrantRoom) ListParticipants(ctx context.Context, room string) ([]Participant, error) {
	if r.listed != nil {
		close(r.listed)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.replacementRoom.ListParticipants(ctx, room)
}

func TestRecoveredAdmissionCancelsItsFailedRemoval(t *testing.T) {
	t.Parallel()
	for _, inflight := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "inflight"}[inflight], func(t *testing.T) {
			r := &retryGrantRoom{}
			s := NewService(Options{Channels: retryGrantChannels{}, Rooms: r, APIKey: "key", APISecret: "secret"})
			room, p := Room{Name: "42", SID: "RM_1"}, Participant{Identity: "1", SID: "PA_new"}
			if err := s.joined(t.Context(), 42, room, p); err == nil {
				t.Fatal("first admission error=nil, want failed update")
			}
			done := make(chan struct{})
			if inflight {
				r.listed, r.release = make(chan struct{}), make(chan struct{})
				defer close(r.release)
				go func() { s.removeDue(t.Context()); close(done) }()
				select {
				case <-r.listed:
				case <-time.After(5 * time.Second):
					t.Fatal("removal did not inspect participant")
				}
			}
			if err := s.joined(t.Context(), 42, room, p); err != nil {
				t.Fatalf("retry admission: %v", err)
			}
			if inflight {
				r.release <- struct{}{}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("removal did not finish")
				}
			} else {
				s.removeDue(t.Context())
			}
			if r.removed {
				t.Fatal("recovered admission was removed by its failed attempt")
			}
		})
	}
}
