// Package db owns the PostgreSQL connection pool, transactions and schema
// migrations shared by all modules.
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultMaxConns sizes the pool. pgx would size it by the CPU count (at
// least 4), few enough on a small box for a burst of gateway READYs to
// queue up behind each other; 16 fits well inside PostgreSQL's default
// max_connections of 100. The pool_max_conns parameter of the database URL
// overrides it. MinConns stays at pgx's default of 0: connections open on
// demand and idle ones close.
const DefaultMaxConns = 16

// DefaultAcquireTimeout bounds the wait for a free pool connection. A pool
// that stays exhausted for that long (a burst, or a bug that wedges it)
// fails the request with context.DeadlineExceeded, which httpx answers 503
// busy, instead of hanging it until the client gives up.
const DefaultAcquireTimeout = 5 * time.Second

// testAcquireTimeout replaces DefaultAcquireTimeout in a test binary. The
// timeout also covers dialing a new connection, and a CI host that runs
// many packages at once, each with its own PostgreSQL container, can take
// longer than five seconds to answer.
const testAcquireTimeout = 30 * time.Second

// defaultAcquireTimeout is the AcquireTimeout of a zero Options.
var defaultAcquireTimeout = func() time.Duration {
	if testing.Testing() {
		return testAcquireTimeout
	}
	return DefaultAcquireTimeout
}()

// ErrInvalidURL means the database URL does not parse. Its text never quotes
// the URL, which may carry the password.
var ErrInvalidURL = errors.New("db: invalid database URL")

// Options tunes OpenWith; zero fields take the defaults.
type Options struct {
	// AcquireTimeout bounds the wait for a pool connection; default
	// DefaultAcquireTimeout, or testAcquireTimeout in a test binary.
	AcquireTimeout time.Duration
}

// Open connects a pool to url with the default options; see OpenWith.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return OpenWith(ctx, url, Options{})
}

// OpenWith connects a pool to url and checks that the database answers.
// Errors never contain the URL or its password.
func OpenWith(ctx context.Context, url string, o Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx's parse errors may quote the connection string.
		return nil, ErrInvalidURL
	}
	// ParseConfig consumes the parameter, so look for it in the URL itself.
	if !strings.Contains(url, "pool_max_conns") {
		cfg.MaxConns = DefaultMaxConns
	}
	if o.AcquireTimeout <= 0 {
		o.AcquireTimeout = defaultAcquireTimeout
	}
	cfg.ConnConfig.Tracer = acquireTimeout{d: o.AcquireTimeout}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// acquireTimeout bounds pgxpool's Acquire through its tracer hook: the
// context TraceAcquireStart returns is used for the acquisition only (the
// query that follows runs on the caller's context), and TraceAcquireEnd
// releases its timer.
type acquireTimeout struct{ d time.Duration }

type cancelKey struct{}

var (
	_ pgxpool.AcquireTracer = acquireTimeout{}
	_ pgx.QueryTracer       = acquireTimeout{}
)

func (a acquireTimeout) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	ctx, cancel := context.WithTimeout(ctx, a.d)
	return context.WithValue(ctx, cancelKey{}, cancel)
}

func (acquireTimeout) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	if cancel, ok := ctx.Value(cancelKey{}).(context.CancelFunc); ok {
		cancel()
	}
}

func (acquireTimeout) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (acquireTimeout) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
