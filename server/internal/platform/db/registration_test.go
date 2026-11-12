package db_test

import (
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMigrationClosesRegistrationOnFreshInstance(t *testing.T) {
	pool := testkit.Postgres(t)
	var open bool
	err := pool.QueryRow(t.Context(), "SELECT registration_open FROM instance").Scan(&open)
	if err != nil || open {
		t.Fatalf("registration_open = %t, %v; want false", open, err)
	}
}

func TestRegistrationMigrationPreservesExistingInstance(t *testing.T) {
	pool := testkit.Postgres(t)
	ctx := t.Context()
	if err := db.MigrateDownTo(ctx, pool, 15); err != nil {
		t.Fatalf("migrate to pre-hardening schema: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO users (id, username, display_name, password_hash, created_at) VALUES (1, 'alice', 'Alice', 'fixture', now())"); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE instance SET registration_open = true"); err != nil {
		t.Fatalf("open registration: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var open bool
	err := pool.QueryRow(ctx, "SELECT registration_open FROM instance").Scan(&open)
	if err != nil || !open {
		t.Fatalf("registration_open = %t, %v; want true", open, err)
	}
}
