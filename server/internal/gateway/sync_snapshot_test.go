package gateway_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// snapshotCounter measures common guild loads without counting personal views.
type snapshotCounter struct {
	guilds.Directory
	calls atomic.Int64
}

func (d *snapshotCounter) Snapshot(ctx context.Context, id snowflake.ID) (guilds.GuildSnapshot, error) {
	d.calls.Add(1)
	return d.Directory.Snapshot(ctx, id)
}

func TestGuildSyncLoadsOneSharedSnapshot(t *testing.T) {
	dir := &snapshotCounter{}
	e := newEnvWith(t, gateway.Options{}, func(m *app.Modules, o *gateway.Options) {
		dir.Directory, o.Guilds = m.Guilds, dir
	})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	g, _ := newGuild(t, alice, "club")
	e.join(g, bu)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	e.untilSentinel(acl, au)
	dir.calls.Store(0)
	e.bus.Publish(t.Context(), events.Event{Type: events.GuildSync,
		Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: parseID(t, g.ID)},
		Payload:  guilds.GuildSyncPayload{GuildID: parseID(t, g.ID)}})
	for _, receiver := range []struct {
		client *wsClient
		user   userBody
	}{{acl, au}, {bcl, bu}} {
		var state guildStateBody
		decode(t, receiver.client.waitFor(events.GuildSync).D, &state)
		if state.MyMember.User.ID != receiver.user.ID {
			t.Errorf("my member = %s, want %s", state.MyMember.User.ID, receiver.user.ID)
		}
	}
	if got := dir.calls.Load(); got != 1 {
		t.Errorf("shared snapshot loads = %d, want 1", got)
	}
}

func TestGuildSyncSkipsSnapshotWhenRecipientsAreOffline(t *testing.T) {
	dir := &snapshotCounter{}
	e := newEnvWith(t, gateway.Options{}, func(m *app.Modules, o *gateway.Options) {
		dir.Directory, o.Guilds = m.Guilds, dir
	})
	alice, au := e.user("alice")
	bob, _ := e.user("bob")
	newGuild(t, alice, "online")
	offline, _ := newGuild(t, bob, "offline")
	client, _ := connect(t, alice)
	e.untilSentinel(client, au)
	dir.calls.Store(0)
	e.bus.Publish(t.Context(), events.Event{Type: events.GuildSync,
		Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: parseID(t, offline.ID)},
		Payload:  guilds.GuildSyncPayload{GuildID: parseID(t, offline.ID)}})
	e.untilSentinel(client, au)
	if got := dir.calls.Load(); got != 0 {
		t.Errorf("offline guild snapshot loads = %d, want 0", got)
	}
}
