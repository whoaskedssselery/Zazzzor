package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrateDownTo rolls migrations back to version (0 = empty schema).
func MigrateDownTo(ctx context.Context, pool *pgxpool.Pool, version int64) error {
	p, err := newProvider(pool)
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	_, err = p.DownTo(ctx, version)
	return err
}

// Version is the current schema version.
func Version(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	p, err := newProvider(pool)
	if err != nil {
		return 0, err
	}
	defer func() { _ = p.Close() }()
	return p.GetDBVersion(ctx)
}
