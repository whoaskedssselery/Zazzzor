package db_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

func TestOpenPingsDatabase(t *testing.T) {
	ctx := t.Context()
	pool, err := db.Open(ctx, testkit.PostgresURL(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("SELECT 1 = %d, %v", one, err)
	}
}

func TestOpenFailsOnUnreachableDatabase(t *testing.T) {
	const url = "postgres://zz:hunter2secret@127.0.0.1:1/zz?connect_timeout=2" //nolint:gosec // fake credentials for a closed port
	pool, err := db.Open(t.Context(), url)
	if err == nil {
		pool.Close()
		t.Fatal("Open succeeded against a closed port")
	}
	if strings.Contains(err.Error(), "hunter2secret") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestMigrateUpDown(t *testing.T) {
	ctx := t.Context()
	pool := freshPool(t)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	up, err := db.Version(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if up < 1 {
		t.Fatalf("version after Up = %d, want >= 1", up)
	}
	// Up again is a no-op.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if err := db.MigrateDownTo(ctx, pool, 0); err != nil {
		t.Fatalf("Down to 0: %v", err)
	}
	if v, err := db.Version(ctx, pool); err != nil || v != 0 {
		t.Fatalf("version after Down = %d, %v; want 0", v, err)
	}
	// And the schema can be rebuilt from scratch.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate after Down: %v", err)
	}
}

func TestPostgresGivesEachTestAMigratedDatabase(t *testing.T) {
	ctx := t.Context()
	a, b := testkit.Postgres(t), testkit.Postgres(t)
	var nameA, nameB string
	if err := a.QueryRow(ctx, "SELECT current_database()").Scan(&nameA); err != nil {
		t.Fatal(err)
	}
	if err := b.QueryRow(ctx, "SELECT current_database()").Scan(&nameB); err != nil {
		t.Fatal(err)
	}
	if nameA == nameB {
		t.Fatalf("two calls share database %q", nameA)
	}
	if v, err := db.Version(ctx, a); err != nil || v < 1 {
		t.Fatalf("template is not migrated: version %d, %v", v, err)
	}
}

func TestInTxCommits(t *testing.T) {
	ctx := t.Context()
	pool := scratchTable(t)
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO scratch (v) VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := countScratch(t, pool); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	ctx := t.Context()
	pool := scratchTable(t)
	boom := errors.New("boom")
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO scratch (v) VALUES (1)"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx error = %v, want boom", err)
	}
	if n := countScratch(t, pool); n != 0 {
		t.Fatalf("rows = %d after rollback, want 0", n)
	}
}

func TestInTxRollsBackOnPanic(t *testing.T) {
	ctx := t.Context()
	pool := scratchTable(t)
	func() {
		defer func() {
			if r := recover(); r != "kaboom" {
				t.Fatalf("recovered %v, want the original panic", r)
			}
		}()
		_ = db.InTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO scratch (v) VALUES (1)"); err != nil {
				return err
			}
			panic("kaboom")
		})
	}()
	if n := countScratch(t, pool); n != 0 {
		t.Fatalf("rows = %d after panic, want 0", n)
	}
	// The pool still works: the connection was released.
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInTxRetriesSerializationFailures(t *testing.T) {
	ctx := t.Context()
	pool := scratchTable(t)
	for _, code := range []string{"40001", "40P01"} {
		var calls atomic.Int32
		err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO scratch (v) VALUES (1)"); err != nil {
				return err
			}
			if calls.Add(1) < 3 {
				return &pgconn.PgError{Code: code}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: InTx = %v", code, err)
		}
		if calls.Load() != 3 {
			t.Fatalf("%s: fn called %d times, want 3", code, calls.Load())
		}
	}
	// Only the successful attempt of each run committed.
	if n := countScratch(t, pool); n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestInTxGivesUpAfterThreeAttempts(t *testing.T) {
	pool := scratchTable(t)
	var calls atomic.Int32
	err := db.InTx(t.Context(), pool, func(pgx.Tx) error {
		calls.Add(1)
		return &pgconn.PgError{Code: "40001"}
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Fatalf("InTx = %v, want the serialization failure", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("fn called %d times, want 3", calls.Load())
	}
}

func TestInTxDoesNotRetryOtherErrors(t *testing.T) {
	pool := scratchTable(t)
	var calls atomic.Int32
	err := db.InTx(t.Context(), pool, func(pgx.Tx) error {
		calls.Add(1)
		return &pgconn.PgError{Code: "23505"}
	})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("InTx = %v after %d calls, want unique violation after 1", err, calls.Load())
	}
}

// freshPool is an empty, unmigrated database.
func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testkit.Postgres(t)
	if err := db.MigrateDownTo(t.Context(), pool, 0); err != nil {
		t.Fatal(err)
	}
	return pool
}

func scratchTable(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testkit.Postgres(t)
	if _, err := pool.Exec(t.Context(), "CREATE TABLE scratch (v int NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func countScratch(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.WithoutCancel(t.Context()), "SELECT count(*) FROM scratch").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func withParam(t *testing.T, raw, key, value string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestOpenSizesPool(t *testing.T) {
	pool, err := db.Open(t.Context(), testkit.PostgresURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if got := pool.Config().MaxConns; got != db.DefaultMaxConns {
		t.Fatalf("MaxConns = %d, want the default %d", got, db.DefaultMaxConns)
	}

	small, err := db.Open(t.Context(), withParam(t, testkit.PostgresURL(t), "pool_max_conns", "3"))
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	if got := small.Config().MaxConns; got != 3 {
		t.Fatalf("MaxConns = %d, want 3 from pool_max_conns", got)
	}
}

// TestAcquireIsBounded: when every connection is taken, a query waits at
// most the acquire timeout and fails with context.DeadlineExceeded, even if
// its own context has no deadline.
func TestAcquireIsBounded(t *testing.T) {
	pool, err := db.OpenWith(t.Context(), withParam(t, testkit.PostgresURL(t), "pool_max_conns", "1"),
		db.Options{AcquireTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	held, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = pool.Exec(context.Background(), "SELECT 1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Exec on a full pool = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Exec waited %s, want about the acquire timeout", d)
	}
	held.Release()
	// Once a connection is free again, queries run normally, and the
	// timeout does not cut a query that got its connection in time.
	if _, err := pool.Exec(context.Background(), "SELECT pg_sleep(0.4)"); err != nil {
		t.Fatalf("Exec after release: %v", err)
	}
}
