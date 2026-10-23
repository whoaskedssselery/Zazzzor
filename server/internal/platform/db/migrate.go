package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Every module adds its own NNNN_<module>.sql file (goose Up/Down) here.
//
//go:embed migrations/*.sql
var embedded embed.FS

// Migrate applies all pending migrations. Concurrent callers (several
// server instances starting at once) are serialised by an advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	p, err := newProvider(pool)
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}

func newProvider(pool *pgxpool.Pool) (*goose.Provider, error) {
	migrations, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("db: migration lock: %w", err)
	}
	// Closing this *sql.DB (via Provider.Close) leaves the pool open.
	p, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), migrations,
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("db: migration provider: %w", err)
	}
	return p, nil
}
