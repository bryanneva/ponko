package sqlite_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/bryanneva/ponko/internal/sqlite"
)

func TestOpen_InMemory(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) error: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestMigrate_CreatesTables(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify all three tables exist
	for _, table := range []string{"tasks", "events", "budget_usage"} {
		var name string
		err := db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not found after migration", table)
		} else if err != nil {
			t.Errorf("querying for table %q: %v", table, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate (idempotency check): %v", err)
	}
}

func TestMigrate_CreatesIndexes(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	indexes := []string{
		"idx_tasks_status",
		"idx_tasks_issue_url",
		"idx_events_task_id",
		"idx_budget_usage_date",
		"idx_budget_usage_run_id",
	}
	for _, idx := range indexes {
		var name string
		err := db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("index %q not found after migration", idx)
		} else if err != nil {
			t.Errorf("querying for index %q: %v", idx, err)
		}
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sub/dir/test.db"
	if err := sqlite.EnsureDir(path); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	// Should be idempotent
	if err := sqlite.EnsureDir(path); err != nil {
		t.Fatalf("EnsureDir (second call): %v", err)
	}
}
