package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DrainRiverQueue waits until there are no runnable River jobs left.
// Scheduled jobs are intentionally excluded so approval waits and snoozed gate
// checks can persist for a future run.
func DrainRiverQueue(ctx context.Context, db *sql.DB) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		var count int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM river_job WHERE state IN ('available', 'running', 'pending', 'retryable')`,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("count runnable river jobs: %w", err)
		}
		if count == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
