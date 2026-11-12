package relationships_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

type recorder struct {
	mu     sync.Mutex
	events []events.Event
}

func (r *recorder) Publish(_ context.Context, e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func setup(t *testing.T, now func() time.Time) (*relationships.Service, *pgxpool.Pool, *recorder) {
	t.Helper()
	pool := testkit.Postgres(t)
	_, err := pool.Exec(t.Context(), `INSERT INTO users(id,username,display_name,password_hash,created_at)
 VALUES (101,'alice','Alice','unused',now()),(102,'bob','Bob','unused',now()),(103,'eve','Eve','unused',now())`)
	if err != nil {
		t.Fatalf("seed users: %v", err)
	}
	pub := &recorder{}
	return relationships.NewService(relationships.Options{Pool: pool, Publisher: pub, Now: now}), pool, pub
}

const alice snowflake.ID = 101
const bob snowflake.ID = 102

func requireStatus(t *testing.T, err error, status int, code string) {
	t.Helper()
	var p *httpx.Problem
	if !errors.As(err, &p) || p.Status != status || p.Code != code {
		t.Fatalf("error = %v, want status %d and code %s", err, status, code)
	}
}

func request(t *testing.T, s *relationships.Service, id snowflake.ID, name string) {
	t.Helper()
	if err := s.Request(t.Context(), id, name); err != nil {
		t.Fatalf("request %s: %v", name, err)
	}
}

func assertView(t *testing.T, s *relationships.Service, id snowflake.ID, want relationships.Type, other snowflake.ID) {
	t.Helper()
	got, err := s.List(t.Context(), id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if want == 0 {
		if got == nil || len(got) != 0 {
			t.Fatalf("relationships = %+v, want []", got)
		}
		return
	}
	if len(got) != 1 || got[0].Type != want || got[0].ID != other || got[0].User.ID != other || got[0].Since == "" {
		t.Fatalf("relationships = %+v, want type %v for %s", got, want, other)
	}
}

func TestRequestCreatesPrivateDirectionalViews(t *testing.T) {
	s, _, pub := setup(t, nil)
	for _, name := range []string{"ALICE", "unknown", "bob" + string(rune(0))} {
		requireStatus(t, s.Request(t.Context(), alice, name), http.StatusNotFound, "not_found")
	}
	request(t, s, alice, "BoB")
	assertView(t, s, alice, relationships.Outgoing, bob)
	assertView(t, s, bob, relationships.Incoming, alice)
	request(t, s, alice, "bob")
	if len(pub.events) != 2 {
		t.Fatalf("events = %d, want 2", len(pub.events))
	}
	for i, want := range []relationships.Type{relationships.Outgoing, relationships.Incoming} {
		e := pub.events[i]
		own, other := []snowflake.ID{alice, bob}[i], []snowflake.ID{bob, alice}[i]
		payload, ok := e.Payload.(relationships.Relationship)
		if e.Type != events.RelationshipAdd || !ok || payload.Type != want || payload.ID != other ||
			e.Audience.Kind != events.AudienceUsers || len(e.Audience.UserIDs) != 1 || e.Audience.UserIDs[0] != own {
			t.Errorf("event = %+v, want type %v private to %s", e, want, own)
		}
	}
	request(t, s, bob, "alice")
	assertView(t, s, alice, relationships.Friend, bob)
	assertView(t, s, bob, relationships.Friend, alice)
}

func TestAcceptDeclineCancelAndRemove(t *testing.T) {
	s, _, pub := setup(t, nil)
	requireStatus(t, s.Put(t.Context(), bob, alice, relationships.Friend), http.StatusNotFound, "not_found")
	for _, scenario := range []string{"decline", "cancel", "unfriend"} {
		request(t, s, alice, "bob")
		actor, other := bob, alice
		wantActor, wantOther := relationships.Incoming, relationships.Outgoing
		if scenario == "cancel" {
			actor, other = alice, bob
			wantActor, wantOther = relationships.Outgoing, relationships.Incoming
		}
		if scenario == "unfriend" {
			if err := s.Put(t.Context(), bob, alice, relationships.Friend); err != nil {
				t.Fatalf("accept: %v", err)
			}
			assertView(t, s, bob, relationships.Friend, alice)
			assertView(t, s, alice, relationships.Friend, bob)
			wantActor, wantOther = relationships.Friend, relationships.Friend
		}
		start := len(pub.events)
		for range 2 {
			if err := s.Delete(t.Context(), actor, other); err != nil {
				t.Fatalf("%s: %v", scenario, err)
			}
		}
		assertView(t, s, alice, 0, 0)
		assertView(t, s, bob, 0, 0)
		added := pub.events[start:]
		if len(added) != 2 {
			t.Fatalf("%s events = %v, want 2 removals", scenario, added)
		}
		for i, want := range []relationships.Type{wantActor, wantOther} {
			event := added[i]
			payload, ok := event.Payload.(relationships.Removed)
			if event.Type != events.RelationshipRemove || !ok || payload.Type != want {
				t.Errorf("%s removal = %+v, want %v", scenario, event, want)
			}
		}
	}
}

func TestBlockIsPrivateAndUnblockingRestoresRequests(t *testing.T) {
	s, _, pub := setup(t, nil)
	request(t, s, alice, "bob")
	request(t, s, bob, "alice")
	start := len(pub.events)
	if err := s.Put(t.Context(), alice, bob, relationships.Blocked); err != nil {
		t.Fatalf("block: %v", err)
	}
	assertView(t, s, alice, relationships.Blocked, bob)
	assertView(t, s, bob, 0, 0)
	requireStatus(t, s.Request(t.Context(), alice, "bob"), http.StatusBadRequest, "blocked_by_you")
	request(t, s, bob, "alice")
	assertView(t, s, bob, 0, 0)
	added := pub.events[start:]
	if len(added) != 2 || added[1].Type != events.RelationshipRemove ||
		len(added[1].Audience.UserIDs) != 1 || added[1].Audience.UserIDs[0] != bob {
		t.Fatalf("block and retry events = %+v, want private block and removal", added)
	}
	if err := s.Delete(t.Context(), alice, bob); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	request(t, s, bob, "alice")
	assertView(t, s, bob, relationships.Outgoing, alice)
	assertView(t, s, alice, relationships.Incoming, bob)
}

func TestRemovingOwnBlockKeepsOtherBlock(t *testing.T) {
	s, _, _ := setup(t, nil)
	for _, pair := range [][2]snowflake.ID{{alice, bob}, {bob, alice}} {
		if err := s.Put(t.Context(), pair[0], pair[1], relationships.Blocked); err != nil {
			t.Fatalf("block: %v", err)
		}
	}
	if err := s.Delete(t.Context(), alice, bob); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	request(t, s, alice, "bob")
	assertView(t, s, alice, 0, 0)
	assertView(t, s, bob, relationships.Blocked, alice)
}

func TestConcurrentRequestsBecomeFriends(t *testing.T) {
	s, pool, pub := setup(t, nil)
	// Different service instances bypass the in-process publication lock.
	other := relationships.NewService(relationships.Options{Pool: pool, Publisher: pub})
	start := make(chan struct{})
	result := make(chan error, 2)
	go func() { <-start; result <- s.Request(t.Context(), alice, "bob") }()
	go func() { <-start; result <- other.Request(t.Context(), bob, "alice") }()
	close(start)
	for range 2 {
		if err := <-result; err != nil {
			t.Fatalf("concurrent request: %v", err)
		}
	}
	assertView(t, s, alice, relationships.Friend, bob)
	assertView(t, s, bob, relationships.Friend, alice)
	var rows int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM relationships").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("count = %d, error = %v, want 2 rows", rows, err)
	}
}

func TestRelationshipCapacity(t *testing.T) {
	for _, tc := range []struct {
		kind  int
		count int
		code  string
	}{
		{1, 1000, "too_many_friends"}, {4, 100, "too_many_outgoing_requests"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			s, pool, _ := setup(t, nil)
			seedRelationships(t, pool, tc.kind, tc.count)
			if tc.kind == 1 {
				request(t, s, bob, "alice")
				requireStatus(t, s.Put(t.Context(), bob, alice, relationships.Friend), http.StatusNotFound, "not_found")
				requireStatus(t, s.Put(t.Context(), alice, bob, relationships.Friend), http.StatusBadRequest, tc.code)
				requireStatus(t, s.Request(t.Context(), alice, "bob"), http.StatusBadRequest, tc.code)
				assertView(t, s, bob, relationships.Outgoing, alice)
			} else {
				requireStatus(t, s.Request(t.Context(), alice, "bob"), http.StatusBadRequest, tc.code)
				assertView(t, s, bob, 0, 0)
			}
		})
	}
}

func TestRequestLimitsAreRollingAndPersisted(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	s, pool, _ := setup(t, clock)
	for batch := range 10 {
		for attempt := range 10 {
			requireStatus(t, s.Request(t.Context(), alice, fmt.Sprintf("missing%d", attempt)), http.StatusNotFound, "not_found")
		}
		requireStatus(t, s.Request(t.Context(), alice, "bob"), http.StatusTooManyRequests, "rate_limited")
		if batch < 9 {
			now = now.Add(time.Minute)
		}
	}
	now = now.Add(time.Minute)
	restarted := relationships.NewService(relationships.Options{Pool: pool, Now: clock})
	requireStatus(t, restarted.Request(t.Context(), alice, "bob"), http.StatusTooManyRequests, "rate_limited")
	now = now.Add(24 * time.Hour)
	request(t, restarted, alice, "bob")
	assertView(t, restarted, bob, relationships.Incoming, alice)
}

func seedRelationships(t *testing.T, pool *pgxpool.Pool, kind, count int) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `INSERT INTO users(id,username,display_name,password_hash,created_at)
    SELECT i,'u'||i,'User','unused',now() FROM generate_series(2000,1999+$1::integer) AS i`, count); err != nil {
		t.Fatalf("seed accounts: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO relationships(user_id,other_id,type,since)
    SELECT 101,id,$1::smallint,now() FROM users WHERE id>=2000 UNION ALL
    SELECT id,101,CASE WHEN $1::smallint=1 THEN 1 ELSE 3 END,now() FROM users WHERE id>=2000`, kind); err != nil {
		t.Fatalf("seed relationships: %v", err)
	}
}
