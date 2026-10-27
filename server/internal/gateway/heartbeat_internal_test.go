package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
)

func TestHeartbeatTimeoutDefault(t *testing.T) {
	if DefaultHeartbeatTimeout <= 2*HeartbeatInterval {
		t.Fatalf("timeout %v leaves no room for a heartbeat delayed past one interval", DefaultHeartbeatTimeout)
	}
	bus := events.NewBus(nil)
	t.Cleanup(bus.Close)
	g := New(Options{Bus: bus})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := g.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if g.opts.HeartbeatTimeout != DefaultHeartbeatTimeout {
		t.Fatalf("timeout = %v, want the default %v", g.opts.HeartbeatTimeout, DefaultHeartbeatTimeout)
	}
}
