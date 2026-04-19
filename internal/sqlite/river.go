package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"
)

// MigrateRiver applies River's schema migrations to the SQLite database.
// It must run before starting a River client and is safe to call repeatedly.
func MigrateRiver(ctx context.Context, db *sql.DB) error {
	migrator, err := rivermigrate.New(riversqlite.New(db), nil)
	if err != nil {
		return fmt.Errorf("creating river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("running river migrations: %w", err)
	}

	return nil
}
