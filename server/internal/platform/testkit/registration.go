package testkit

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenRegistration enables public signup in a disposable test database.
func OpenRegistration(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), "UPDATE instance SET registration_open = true"); err != nil {
		t.Fatalf("testkit: open registration: %v", err)
	}
}
