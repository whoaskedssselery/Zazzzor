package relationships_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships"
)

func TestConcurrentRequestsShareOutgoingCapacity(t *testing.T) {
	s, pool, pub := setup(t, nil)
	seedRelationships(t, pool, 4, 99)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, `
  CREATE FUNCTION hold_outgoing_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
  BEGIN
    IF NEW.user_id = 101 AND NEW.type = 4 THEN
      PERFORM pg_advisory_xact_lock(917150224);
    END IF;
    RETURN NEW;
  END;
  $$;
  CREATE TRIGGER hold_outgoing_for_test BEFORE INSERT ON relationships
  FOR EACH ROW EXECUTE FUNCTION hold_outgoing_for_test();
 `)
	if err != nil {
		t.Fatalf("install insert barrier: %v", err)
	}
	barrier, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin barrier: %v", err)
	}
	defer func() { _ = barrier.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = barrier.Exec(ctx, "SELECT pg_advisory_xact_lock(917150224)"); err != nil {
		t.Fatalf("hold insert barrier: %v", err)
	}
	other := relationships.NewService(relationships.Options{Pool: pool, Publisher: pub})
	result := make(chan error, 2)
	go func() { result <- s.Request(ctx, alice, "bob") }()
	go func() { result <- other.Request(ctx, alice, "eve") }()
	// With the account lock, one transaction waits before counting. Without
	// it, both count 99 before blocking inside their respective INSERTs.
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		queryErr := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
    WHERE datname = current_database() AND wait_event_type = 'Lock'
    AND (query LIKE '-- name: LockRelationshipUsers%' OR query LIKE '-- name: SaveRelationship%')`).Scan(&waiting)
		if queryErr != nil {
			t.Fatalf("observe waiting transactions: %v", queryErr)
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both writes did not reach the barrier")
		case <-tick.C:
		}
	}
	if err = barrier.Commit(ctx); err != nil {
		t.Fatalf("release insert barrier: %v", err)
	}
	var rejected int
	for range 2 {
		if got := <-result; got != nil {
			requireStatus(t, got, http.StatusBadRequest, "too_many_outgoing_requests")
			rejected++
		}
	}
	if rejected != 1 {
		t.Errorf("quota rejections = %d, want 1", rejected)
	}
}

func TestConcurrentRequestsShareRateWindow(t *testing.T) {
	s, pool, _ := setup(t, nil)
	other := relationships.NewService(relationships.Options{Pool: pool})
	done := make(chan error, 11)
	for i := range 11 {
		worker := s
		if i%2 == 0 {
			worker = other
		}
		go func() { done <- worker.Request(t.Context(), alice, "missing") }()
	}
	var missing, limited int
	for range 11 {
		var p *httpx.Problem
		if err := <-done; !errors.As(err, &p) {
			t.Fatalf("request = %v, want 404 or 429", err)
		}
		switch p.Status {
		case http.StatusNotFound:
			missing++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("status = %d, want 404 or 429", p.Status)
		}
	}
	if missing != 10 || limited != 1 {
		t.Errorf("404 = %d, 429 = %d, want 10 and 1", missing, limited)
	}
}
