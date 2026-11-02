package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

type syncChannels struct {
	voice.Channels
	mask atomic.Uint64
}

func (*syncChannels) Lookup(context.Context, snowflake.ID) (channels.Channel, error) {
	return channels.Channel{ID: 42, GuildID: 7, Type: channels.TypeVoice}, nil
}

func (c *syncChannels) Permissions(context.Context, snowflake.ID, snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	return permissions.Permission(c.mask.Load()), 7, nil
}

type syncRooms struct {
	voice.RoomClient
	updates chan voice.ParticipantPermissions
}

func (*syncRooms) ListRooms(context.Context) ([]voice.Room, error) {
	return []voice.Room{{Name: "42", SID: "RM_1"}}, nil
}

func (*syncRooms) ListParticipants(context.Context, string) ([]voice.Participant, error) {
	return []voice.Participant{{Identity: "1", SID: "PA_1"}}, nil
}

func (r *syncRooms) UpdateParticipant(_ context.Context, _, _ string, p voice.ParticipantPermissions) error {
	r.updates <- p
	return nil
}

func TestVoiceCleanupQueuesPermissionRefresh(t *testing.T) {
	c := &syncChannels{}
	c.mask.Store(uint64(permissions.ViewChannel | permissions.Connect | permissions.Speak))
	r := &syncRooms{updates: make(chan voice.ParticipantPermissions, 8)}
	bus := events.NewBus(nil)
	frames, unsubscribe := bus.Subscribe(8)
	defer unsubscribe()
	v := voice.NewService(voice.Options{Channels: c, Rooms: r, APIKey: "key", APISecret: "secret", Publisher: bus})
	if err := v.Reconcile(t.Context()); err != nil {
		t.Fatalf("seed participant: %v", err)
	}
	<-r.updates
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { v.Run(ctx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("voice worker did not stop")
		}
	}()
	select {
	case <-r.updates: // Run's initial reconcile precedes the permission change.
	case <-time.After(5 * time.Second):
		t.Fatal("voice worker did not start")
	}
	c.mask.Store(uint64(permissions.ViewChannel | permissions.Connect))
	p := &voiceCleanup{next: bus, voice: v}
	p.Publish(t.Context(), events.Event{Type: events.GuildSync, Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: 7}})
	select {
	case grants := <-r.updates:
		if len(grants.Sources) != 0 {
			t.Fatalf("refreshed grants=%+v, want no publish sources", grants)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GUILD_SYNC did not trigger immediate voice permission refresh")
	}
	for {
		select {
		case ev := <-frames:
			if ev.Type == events.GuildSync {
				return
			}
		default:
			t.Fatal("publisher did not forward GUILD_SYNC")
		}
	}
}
