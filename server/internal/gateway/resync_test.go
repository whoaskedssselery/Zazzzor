package gateway_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// failingDirectory fails MemberIDs of one guild while armed.
type failingDirectory struct {
	guilds.Directory
	guild atomic.Int64
}

func (d *failingDirectory) MemberIDs(ctx context.Context, guildID snowflake.ID) ([]snowflake.ID, error) {
	if int64(guildID) == d.guild.Load() {
		return nil, errors.New("directory: injected failure")
	}
	return d.Directory.MemberIDs(ctx, guildID)
}

// TestUnresolvedGuildEventResyncsItsMembers: when the recipients of a
// guild event cannot be resolved, the connections that hold that guild
// are closed with 4008 so their clients reconnect and resync; others stay.
func TestUnresolvedGuildEventResyncsItsMembers(t *testing.T) {
	t.Parallel()
	dir := &failingDirectory{}
	e := newEnvWith(t, gateway.Options{}, func(m *app.Modules, o *gateway.Options) {
		dir.Directory = m.Guilds
		o.Guilds = dir
	})
	e.failsDispatch = true
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	newGuild(t, carol, "elsewhere")
	e.join(g, bu)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)

	dir.guild.Store(int64(parseID(t, g.ID)))
	expect(t, alice.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID, map[string]string{"name": "renamed"}), http.StatusOK)
	for name, cl := range map[string]*wsClient{"alice": acl, "bob": bcl} {
		if code := cl.closeStatus(); code != gateway.CloseOverloaded {
			t.Fatalf("%s closed with %d, want 4008", name, code)
		}
	}
	e.untilSentinel(ccl, cu) // carol, in no affected guild, is still connected
	if n := e.metric("zazzzor_gateway_dispatch_errors_total", ""); n != 1 {
		t.Fatalf("dispatch errors = %v, want 1", n)
	}
}
