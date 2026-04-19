package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Migrate creates all required tables and indexes if they do not already exist.
// It is safe to call multiple times (idempotent).
func Migrate(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			id           TEXT PRIMARY KEY,
			issue_url    TEXT NOT NULL,
			repo         TEXT NOT NULL DEFAULT '',
			issue_number INTEGER NOT NULL DEFAULT 0,
			title        TEXT NOT NULL DEFAULT '',
			labels       TEXT NOT NULL DEFAULT '[]',
			body         TEXT NOT NULL DEFAULT '',
			workflow     TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'queued',
			phase        TEXT NOT NULL DEFAULT '',
			block_reason TEXT NOT NULL DEFAULT '',
			attempts     INTEGER NOT NULL DEFAULT 0,
			last_error   TEXT NOT NULL DEFAULT '',
			cost_usd     REAL NOT NULL DEFAULT 0,
			locked_by    TEXT NOT NULL DEFAULT '',
			locked_at    DATETIME,
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS idx_tasks_status    ON tasks (status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_issue_url ON tasks (issue_url)`,

		`CREATE TABLE IF NOT EXISTS events (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp      DATETIME NOT NULL,
			type           TEXT NOT NULL,
			task_id        TEXT NOT NULL DEFAULT '',
			correlation_id TEXT NOT NULL DEFAULT '',
			payload        TEXT NOT NULL DEFAULT '{}'
		)`,

		`CREATE INDEX IF NOT EXISTS idx_events_task_id ON events (task_id)`,

		`CREATE TABLE IF NOT EXISTS budget_usage (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id  TEXT NOT NULL DEFAULT '',
			run_id   TEXT NOT NULL DEFAULT '',
			cost_usd REAL NOT NULL DEFAULT 0,
			date     TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE INDEX IF NOT EXISTS idx_budget_usage_date   ON budget_usage (date)`,
		`CREATE INDEX IF NOT EXISTS idx_budget_usage_run_id ON budget_usage (run_id)`,

		`CREATE TABLE IF NOT EXISTS pipelines (
			id                        TEXT PRIMARY KEY,
			task_id                   TEXT NOT NULL DEFAULT '',
			issue_url                 TEXT NOT NULL DEFAULT '',
			repo                      TEXT NOT NULL DEFAULT '',
			issue_number              INTEGER NOT NULL DEFAULT 0,
			issue_title               TEXT NOT NULL DEFAULT '',
			issue_body                TEXT NOT NULL DEFAULT '',
			track                     TEXT NOT NULL DEFAULT '',
			stage                     TEXT NOT NULL DEFAULT 'pending',
			plan_output               TEXT NOT NULL DEFAULT '',
			story_count               INTEGER NOT NULL DEFAULT 0,
			stories_completed         INTEGER NOT NULL DEFAULT 0,
			current_story_index       INTEGER NOT NULL DEFAULT 0,
			pr_number                 INTEGER NOT NULL DEFAULT 0,
			classification_rationale  TEXT NOT NULL DEFAULT '',
			cost_usd                  REAL NOT NULL DEFAULT 0,
			created_at                DATETIME NOT NULL,
			updated_at                DATETIME NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS idx_pipelines_stage     ON pipelines (stage)`,
		`CREATE INDEX IF NOT EXISTS idx_pipelines_issue_url ON pipelines (issue_url)`,

		// ALTER TABLE fallbacks for existing databases that predate these columns.
		// SQLite returns "duplicate column name" on re-add; we ignore that error below.
		`ALTER TABLE pipelines ADD COLUMN issue_title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pipelines ADD COLUMN issue_body  TEXT NOT NULL DEFAULT ''`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// Ignore "duplicate column name" errors from ALTER TABLE on existing DBs.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("migrate: %w", err)
		}
	}

	return nil
}
