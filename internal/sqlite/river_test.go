package sqlite_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/sqlite"
)

func TestMigrateRiverCreatesJobTable(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := sqlite.MigrateRiver(ctx, db); err != nil {
		t.Fatalf("MigrateRiver: %v", err)
	}

	var name string
	if err := db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='river_job'",
	).Scan(&name); err != nil {
		t.Fatalf("query river_job table: %v", err)
	}
	if name != "river_job" {
		t.Fatalf("table name = %q, want river_job", name)
	}
}
