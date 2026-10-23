package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txAttempts is how many times InTx runs fn when the database asks for a
// retry.
const txAttempts = 3

// InTx runs fn inside a transaction and commits it if fn returns nil. On an
// error or a panic in fn the transaction is rolled back (and the panic
// re-raised). When the transaction fails with serialization_failure or
// deadlock_detected, the whole transaction, fn included, is retried, up to
// txAttempts runs in total; fn must therefore have no side effects outside
// tx.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = runTx(ctx, pool, fn)
		if err == nil || !retryable(err) || attempt == txAttempts {
			return err
		}
		// Short jittered backoff so the competing transaction can finish.
		delay := time.Duration(attempt)*5*time.Millisecond + rand.N(5*time.Millisecond) //nolint:gosec // jitter, not security
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(delay):
		}
	}
}

func runTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		// Rollback with a live context, so a cancelled request still
		// releases its connection cleanly.
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}

func retryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "40001", // serialization_failure
		"40P01": // deadlock_detected
		return true
	}
	return false
}
