package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDB creates a connection pool to the test database.
// It skips the test in short mode since a running Postgres is required.
// The pool is closed automatically when the test finishes.
func TestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://agent:agent@localhost:5433/agent_dev?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping test database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func EnsureUsersTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			slack_user_id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL DEFAULT '',
			timezone TEXT NOT NULL DEFAULT '',
			is_admin BOOLEAN NOT NULL DEFAULT false,
			cached_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
}
