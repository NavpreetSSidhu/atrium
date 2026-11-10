// Package dbtest gives integration tests a fresh Postgres database with every migration applied.
package dbtest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates database atrium_test_<name> from TEST_DATABASE_URL, applies migrations/*.sql (Up
// sections), and returns a pool on it. Use a distinct name per package: `go test ./...` runs
// packages in parallel. Skips the test when TEST_DATABASE_URL is unset.
func New(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	// Serialize setup across packages: migrations create the cluster-wide app_user role.
	mustExec(t, admin, "select pg_advisory_lock(7461)")

	dbName := "atrium_test_" + name
	mustExec(t, admin, "drop database if exists "+dbName+" with (force)")
	mustExec(t, admin, "create database "+dbName)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, here, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(here), "../../../migrations/*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	for _, f := range files { // Glob returns names sorted, so migrations run in order.
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, strings.Split(string(b), "-- +goose Down")[0]); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
	}
	return pool
}

func mustExec(t *testing.T, c *pgx.Conn, sql string) {
	t.Helper()
	if _, err := c.Exec(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
}
