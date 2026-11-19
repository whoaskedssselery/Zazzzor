package gateway_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type oneFailedDMViewerLookup struct {
	channels.Resolver
	channel atomic.Int64
}

func (f *oneFailedDMViewerLookup) ViewersAmong(ctx context.Context, id snowflake.ID, candidates []snowflake.ID) ([]snowflake.ID, map[snowflake.ID]bool, error) {
	if f.channel.CompareAndSwap(int64(id), 0) {
		return nil, nil, errors.New("viewers: injected private lookup failure")
	}
	return f.Resolver.ViewersAmong(ctx, id, candidates)
}

func TestFailedDMReopenResyncsClosedRecipient(t *testing.T) {
	viewers := &oneFailedDMViewerLookup{}
	e := newEnvWith(t, gateway.Options{}, func(m *app.Modules, o *gateway.Options) {
		viewers.Resolver = m.Channels
		o.Channels = viewers
	})
	e.failsDispatch = true
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	makeFriends(t, alice, bob, a, b)
	ch := openDM(t, alice, b.ID, http.StatusCreated)
	path := "/api/v1/channels/" + ch.ID
	bc, _ := connect(t, bob)
	dmExpect(t, bob.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	bc.waitFor(dmDeleted)
	viewers.channel.Store(int64(parseID(t, ch.ID)))
	sendDM(t, alice, path, map[string]string{"content": "reopen"})
	if code := bc.closeStatus(); code != gateway.CloseOverloaded {
		t.Fatalf("close status = %d, want resync after missed reopen", code)
	}
	readDMReady(t, bob, ch.ID, 1)
}
