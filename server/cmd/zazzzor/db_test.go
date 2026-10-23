package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

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

	pool, err := openDatabase(ctx, url)
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
	pool, err = openDatabase(ctx, url)
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
}
