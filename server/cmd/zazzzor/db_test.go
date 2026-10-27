package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

func TestReadyzPingsDatabase(t *testing.T) {
	pool := testkit.Postgres(t)
	h := newRouter(routerDeps{
		Config:  testConfig(),
		Logger:  slog.New(slog.DiscardHandler),
		Metrics: newHTTPMetrics(prometheus.NewRegistry()),
		Ready:   dbReady(pool),
	})
	c := testkit.NewClient(t, h)

	if resp := c.Do(http.MethodGet, "/api/v1/readyz", nil); resp.Status != http.StatusOK {
		t.Fatalf("readyz with a live database = %d: %s", resp.Status, resp.Body)
	}

	pool.Close()
	resp := c.Do(http.MethodGet, "/api/v1/readyz", nil)
	if resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a closed pool = %d, want 503", resp.Status)
	}
	var p struct{ Code string }
	resp.JSON(&p)
	if p.Code != "not_ready" {
		t.Fatalf("code = %q, want not_ready", p.Code)
	}
}

func TestReadyzTimesOutSlowDatabase(t *testing.T) {
	start := time.Now()
	err := dbReady(slowPinger{})(context.Background())
	if err == nil {
		t.Fatal("slow ping reported ready")
	}
	if d := time.Since(start); d > readyTimeout+time.Second {
		t.Fatalf("ping took %v, want about %v", d, readyTimeout)
	}
}

type slowPinger struct{}

func (slowPinger) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestOpenDatabaseRunsMigrations(t *testing.T) {
	ctx := t.Context()
	url := testkit.EmptyPostgresURL(t)

	pool, err := openDatabase(ctx, slog.New(slog.DiscardHandler), url)
	if err != nil {
		t.Fatalf("openDatabase on an empty database: %v", err)
	}
	t.Cleanup(pool.Close)
	got := schemaVersion(t, pool)

	// It reaches the same version as the fully migrated test template.
	if want := schemaVersion(t, testkit.Postgres(t)); got != want || got < 1 {
		t.Fatalf("schema version after start-up = %d, want %d", got, want)
	}

	// A restart on the migrated database is a no-op.
	pool.Close()
	pool, err = openDatabase(ctx, slog.New(slog.DiscardHandler), url)
	if err != nil {
		t.Fatalf("openDatabase on a migrated database: %v", err)
	}
	t.Cleanup(pool.Close)
	if v := schemaVersion(t, pool); v != got {
		t.Fatalf("schema version after restart = %d, want %d", v, got)
	}
}

func schemaVersion(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var v int64
	err := pool.QueryRow(t.Context(),
		"SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&v)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	return v
}

func TestRunFailsWhenDatabaseIsUnreachable(t *testing.T) {
	defer swapRetryDelay(10 * time.Millisecond)()
	defer swapStartupTimeout(time.Second)()
	const deadURL = "postgres://zz:pw@127.0.0.1:1/zz?connect_timeout=2" //nolint:gosec // fake credentials for a closed port
	var stderr bytes.Buffer
	env := func(k string) string {
		return map[string]string{
			"ZAZZZOR_DATABASE_URL":    deadURL,
			"ZAZZZOR_ALLOWED_ORIGINS": "https://zazzzor.test",
			"ZAZZZOR_HTTP_ADDR":       "127.0.0.1:0",
			"ZAZZZOR_METRICS_ADDR":    "127.0.0.1:0",
		}[k]
	}
	if code := run(nil, env, io.Discard, &stderr); code != 1 {
		t.Fatalf("run exit = %d, want 1", code)
	}
	logs := stderr.String()
	if !strings.Contains(logs, "open database") {
		t.Errorf("stderr does not explain the failure: %s", logs)
	}
	if strings.Contains(logs, `"starting"`) {
		t.Errorf("server started listening despite the database being down: %s", logs)
	}
	if strings.Contains(logs, ":pw@") {
		t.Errorf("logs leak the database password: %s", logs)
	}
	if !strings.Contains(logs, "database not ready") {
		t.Errorf("stderr does not show the retries: %s", logs)
	}
}

func TestRetryOpenWaitsForDatabase(t *testing.T) {
	defer swapRetryDelay(time.Millisecond)()
	var logs bytes.Buffer
	calls := 0
	got, err := retryOpen(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)),
		func(context.Context) (string, error) {
			calls++
			if calls < 3 {
				return "", errors.New("connection refused")
			}
			return "pool", nil
		})
	if err != nil || got != "pool" {
		t.Fatalf("retryOpen = %q, %v; want pool, nil", got, err)
	}
	if calls != 3 {
		t.Fatalf("open called %d times, want 3", calls)
	}
	if n := strings.Count(logs.String(), "database not ready"); n != 2 {
		t.Fatalf("logged %d retries, want 2:\n%s", n, logs.String())
	}
}

func TestRetryOpenGivesUpWithContext(t *testing.T) {
	defer swapRetryDelay(10 * time.Millisecond)()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	calls := 0
	// The first attempt is refused; the next ones hang until the overall
	// deadline, so the returned error must still name the real cause.
	_, err := retryOpen(ctx, slog.New(slog.DiscardHandler),
		func(ctx context.Context) (int, error) {
			calls++
			if calls == 1 {
				return 0, errors.New("connection refused")
			}
			<-ctx.Done()
			return 0, ctx.Err()
		})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want the last open error", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("gave up after %v, want about 200ms", d)
	}
}

func TestRetryOpenStopsOnPermanentErrors(t *testing.T) {
	defer swapRetryDelay(time.Millisecond)()
	for _, cause := range []error{
		db.ErrInvalidURL,
		fmt.Errorf("db: ping: %w", &pgconn.PgError{Code: "28P01"}), // wrong password
		fmt.Errorf("db: ping: %w", &pgconn.PgError{Code: "3D000"}), // no such database
	} {
		calls := 0
		_, err := retryOpen(context.Background(), slog.New(slog.DiscardHandler),
			func(context.Context) (int, error) { calls++; return 0, cause })
		if !errors.Is(err, cause) || calls != 1 {
			t.Errorf("%v: err = %v after %d calls, want it after 1", cause, err, calls)
		}
	}
}

func TestRetryDelayBackoff(t *testing.T) {
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i, w := range want {
		if d := retryDelay(i + 1); d != w {
			t.Errorf("retryDelay(%d) = %v, want %v", i+1, d, w)
		}
	}
}

func swapStartupTimeout(d time.Duration) func() {
	old := startupDBTimeout
	startupDBTimeout = d
	return func() { startupDBTimeout = old }
}

func swapRetryDelay(d time.Duration) func() {
	old := dbRetryDelay
	dbRetryDelay = func(int) time.Duration { return d }
	return func() { dbRetryDelay = old }
}

func TestOpenDatabaseFailsFastOnWrongPassword(t *testing.T) {
	u, err := url.Parse(testkit.EmptyPostgresURL(t))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(u.User.Username(), "wrong-password")
	start := time.Now()
	_, err = openDatabase(t.Context(), slog.New(slog.DiscardHandler), u.String())
	if err == nil {
		t.Fatal("openDatabase with a wrong password succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("wrong password retried for %v, want a fast failure", d)
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

// A restore compares the schema version of a backup with what
// `zazzzor schema-version` prints, so it must be the version the server
// migrates a database to.
func TestSchemaVersionCommandPrintsMigratedVersion(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"schema-version"}, func(string) string { return "" }, &out, io.Discard); code != 0 {
		t.Fatalf("schema-version exit = %d", code)
	}
	want := fmt.Sprint(schemaVersion(t, testkit.Postgres(t)))
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("schema-version printed %q, want %q", got, want)
	}
}
