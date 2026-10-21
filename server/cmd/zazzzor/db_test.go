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
	url := testkit.PostgresURL(t)
	pool, err := openDatabase(ctx, url)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	t.Cleanup(pool.Close)
	// Forget the schema history, as on a brand-new database.
	if _, err = pool.Exec(ctx, "DROP TABLE goose_db_version"); err != nil {
		t.Fatal(err)
	}
	pool.Close()

	pool, err = openDatabase(ctx, url)
	if err != nil {
		t.Fatalf("openDatabase on a blank schema: %v", err)
	}
	t.Cleanup(pool.Close)
	var applied int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0 AND is_applied").Scan(&applied); err != nil {
		t.Fatalf("migrations were not applied: %v", err)
	}
	if applied < 1 {
		t.Fatalf("applied migrations = %d", applied)
	}
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
