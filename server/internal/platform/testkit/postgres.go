// Package testkit is the shared test harness: a real PostgreSQL in a
// container, and an HTTP client that checks every response against the API
// contract. It is imported only from _test.go files.
package testkit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
)

// PostgresImage is the PostgreSQL image tests run against. Keep its tag equal
// to POSTGRES_VERSION in deploy/.env.example (and deploy/.env.ci), which
// deploy/docker-compose.yml runs as postgres:${POSTGRES_VERSION}.
const PostgresImage = "postgres:18.6-alpine"

// Label marks every container testkit starts, so stray ones can be found
// and removed with `docker rm -f $(docker ps -aq --filter label=zazzzor.test=1)`.
const Label = "zazzzor.test"

const (
	templateDB     = "zz_template"
	startTimeout   = 3 * time.Minute
	stopTimeout    = time.Minute
	createDBTimout = 30 * time.Second
)

var (
	mainActive atomic.Bool

	pgOnce   sync.Once
	pg       *sharedPostgres
	errPG    error
	pgClosed atomic.Bool

	dbSeq atomic.Int64
)

// sharedPostgres is the one container of a test binary (= package).
type sharedPostgres struct {
	container *tcpostgres.PostgresContainer
	admin     *pgxpool.Pool // connected to the "postgres" maintenance DB
	url       *url.URL      // admin URL; the path is swapped per database

	createMu sync.Mutex // CREATE DATABASE ... TEMPLATE needs an idle template
}

// Main runs the package's tests and then removes the PostgreSQL container
// if a test started one. Use it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }
func Main(m *testing.M) int {
	mainActive.Store(true)
	// testcontainers logs every container step to stderr; keep test output
	// clean. Startup errors are still returned to the test.
	tclog.SetDefault(tclog.NewNoopLogger())

	code := m.Run()

	pgClosed.Store(true)
	if err := stopPostgres(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "testkit:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// Postgres returns a pool connected to a fresh database of its own, cloned
// from a template that has all migrations applied. The database is dropped
// when t ends. The container starts on first use and is shared by all tests
// of the package.
func Postgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	u := PostgresURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), createDBTimout)
	defer cancel()
	pool, err := db.Open(ctx, u)
	if err != nil {
		t.Fatalf("testkit: connect to test database: %v", err)
	}
	// Cleanups run last-in first-out: the pool closes before the drop.
	t.Cleanup(pool.Close)
	return pool
}

// PostgresURL creates a fresh migrated database like Postgres and returns
// its connection URL instead of a pool.
func PostgresURL(t testing.TB) string {
	t.Helper()
	return newDatabase(t, templateDB)
}

// EmptyPostgresURL creates a fresh database with no migrations applied (cloned
// from template0) and returns its URL. It is dropped when t ends.
func EmptyPostgresURL(t testing.TB) string {
	t.Helper()
	return newDatabase(t, "template0")
}

func newDatabase(t testing.TB, template string) string {
	t.Helper()
	if !mainActive.Load() {
		t.Fatal("testkit: add `func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }` " +
			"to the package so its PostgreSQL container is removed after the tests")
	}
	if pgClosed.Load() {
		t.Fatal("testkit: PostgreSQL requested after the package's tests finished")
	}
	pgOnce.Do(func() { pg, errPG = startPostgres() })
	if errPG != nil {
		t.Fatalf("testkit: start PostgreSQL: %v", errPG)
	}

	name := fmt.Sprintf("zz_test_%d", dbSeq.Add(1))
	ctx, cancel := context.WithTimeout(context.Background(), createDBTimout)
	defer cancel()
	if err := pg.createDatabase(ctx, name, template); err != nil {
		t.Fatalf("testkit: create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), createDBTimout)
		defer cancel()
		if _, err := pg.admin.Exec(ctx, "DROP DATABASE "+quote(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("testkit: drop database %s: %v", name, err)
		}
	})
	return pg.urlFor(name)
}

func startPostgres() (*sharedPostgres, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	ctr, err := tcpostgres.Run(ctx, PostgresImage,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("zazzzor"),
		tcpostgres.WithPassword("zazzzor"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithLabels(map[string]string{Label: "1"}),
		// Durability is pointless for throwaway data; trade it for speed.
		testcontainers.WithCmd("postgres",
			"-c", "fsync=off",
			"-c", "synchronous_commit=off",
			"-c", "full_page_writes=off",
			"-c", "max_connections=500",
		),
	)
	if err != nil {
		if ctr != nil {
			_ = testcontainers.TerminateContainer(ctr)
		}
		return nil, err
	}
	s := &sharedPostgres{container: ctr}
	if err := s.init(ctx); err != nil {
		_ = s.stop()
		return nil, err
	}
	return s, nil
}

func (s *sharedPostgres) init(ctx context.Context) error {
	raw, err := s.container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("connection string: %w", err)
	}
	if s.url, err = url.Parse(raw); err != nil {
		return fmt.Errorf("parse connection string: %w", err)
	}
	if s.admin, err = db.Open(ctx, raw); err != nil {
		return err
	}

	// Build the template once: migrate it, then forbid connections so that
	// CREATE DATABASE ... TEMPLATE never finds it busy.
	if _, err = s.admin.Exec(ctx, "CREATE DATABASE "+templateDB); err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	tpl, err := db.Open(ctx, s.urlFor(templateDB))
	if err != nil {
		return err
	}
	err = db.Migrate(ctx, tpl)
	tpl.Close()
	if err != nil {
		return fmt.Errorf("migrate template: %w", err)
	}
	if _, err := s.admin.Exec(ctx, "ALTER DATABASE "+templateDB+" WITH IS_TEMPLATE true ALLOW_CONNECTIONS false"); err != nil {
		return fmt.Errorf("seal template: %w", err)
	}
	return nil
}

func (s *sharedPostgres) createDatabase(ctx context.Context, name, template string) error {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	_, err := s.admin.Exec(ctx, "CREATE DATABASE "+quote(name)+" TEMPLATE "+quote(template))
	return err
}

func (s *sharedPostgres) urlFor(name string) string {
	u := *s.url
	u.Path = "/" + name
	return u.String()
}

func (s *sharedPostgres) stop() error {
	if s.admin != nil {
		s.admin.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	return testcontainers.TerminateContainer(s.container, testcontainers.StopContext(ctx))
}

func stopPostgres() error {
	// Make sure a concurrent first use cannot start a container after this.
	pgOnce.Do(func() { errPG = errors.New("testkit: tests already finished") })
	if pg == nil {
		return nil
	}
	if err := pg.stop(); err != nil {
		return fmt.Errorf("remove PostgreSQL container: %w", err)
	}
	return nil
}

func quote(ident string) string { return pgx.Identifier{ident}.Sanitize() }
