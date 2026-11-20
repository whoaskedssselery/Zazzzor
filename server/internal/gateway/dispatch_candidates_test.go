package gateway_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// dispatchViewers records whether dispatch requests a whole-guild audience.
type dispatchViewers struct {
	channels.Resolver
	wholeGuild atomic.Int64
	candidates atomic.Int64
}

func (v *dispatchViewers) Viewers(ctx context.Context, id snowflake.ID) ([]snowflake.ID, error) {
	v.wholeGuild.Add(1)
	return v.Resolver.Viewers(ctx, id)
}

func (v *dispatchViewers) ViewersAmong(ctx context.Context, id snowflake.ID, candidates []snowflake.ID) ([]snowflake.ID, map[snowflake.ID]bool, error) {
	v.candidates.Store(int64(len(candidates)))
	return v.Resolver.ViewersAmong(ctx, id, candidates)
}

func TestChannelDispatchChecksOnlyConnectedCandidates(t *testing.T) {
	t.Parallel()
	v := &dispatchViewers{}
	e := newEnvWith(t, gateway.Options{}, func(m *app.Modules, o *gateway.Options) {
		v.Resolver, o.Channels = m.Channels, v
	})
	alice, au := e.user("alice")
	_, offline := e.user("offline")
	g, ch := newGuild(t, alice, "club")
	e.join(g, offline)
	client, _ := connect(t, alice)
	e.bus.Publish(t.Context(), events.Event{Type: events.TypingStart,
		Audience: events.Audience{Kind: events.AudienceChannel, GuildID: parseID(t, g.ID), ChannelID: parseID(t, ch.ID)},
		Payload: map[string]string{"channel_id": ch.ID, "guild_id": g.ID, "user_id": au.ID,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano)}})
	client.waitFor(events.TypingStart)
	if got := v.wholeGuild.Load(); got != 0 {
		t.Errorf("whole-guild viewer loads = %d, want 0", got)
	}
	if got := v.candidates.Load(); got != 1 {
		t.Errorf("permission candidates = %d, want 1 connected user", got)
	}
}
