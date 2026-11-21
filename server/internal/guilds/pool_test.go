package guilds_test

import (
	"context"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// TestSmallPoolDoesNotWedge runs many snapshots and nickname changes at
// once over a two-connection pool. Each holds a transaction while it loads
// the members' accounts from auth; if that lookup took a second connection
// from the pool instead of using the transaction, the callers would each
// hold one connection and wait forever for another.
func TestSmallPoolDoesNotWedge(t *testing.T) {
	t.Parallel()
	u, err := url.Parse(testkit.PostgresURL(t))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("pool_max_conns", "2")
	u.RawQuery = q.Encode()
	pool, err := db.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if got := pool.Config().MaxConns; got != 2 {
		t.Fatalf("MaxConns = %d, want 2 from the URL", got)
	}

	e := newEnvOn(t, pool)
	_, ub := e.user("owner")
	ownerID := ub.id(t)
	owner, err := e.auth.GetUser(t.Context(), ownerID)
	if err != nil {
		t.Fatal(err)
	}
	g, err := e.svc.CreateGuild(t.Context(), owner, "Small pool", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range e.rawUsers(3) {
		e.join(g.Guild.ID, id)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	const n = 16
	errs := make(chan error, 2*n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, err := e.svc.Snapshot(ctx, g.Guild.ID)
			errs <- err
		})
		wg.Go(func() {
			nick := "nick" + strconv.Itoa(i)
			p := &nick
			_, err := e.svc.UpdateMember(ctx, g.Guild.ID, ownerID, ownerID, guilds.MemberFields{Nick: &p})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent snapshot/update over a 2-connection pool: %v", err)
		}
	}
}
