package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// isAlreadyExists checks if a Postgres error indicates a relation already exists (SQLSTATE 42P07).
func isAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 42P07")
}

// Migrate runs all SQL migration files from the given directory.
// It tracks applied migrations in a schema_migrations table and
// only runs each migration once (idempotent).
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, file := range files {
		var exists bool
		err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", file).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", file, err)
		}
		if exists {
			continue
		}

		content, err := os.ReadFile(filepath.Join(migrationsDir, file))
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", file, err)
		}

		upSQL := extractGooseUp(string(content))
		if upSQL == "" {
			continue
		}

		_, execErr := pool.Exec(ctx, upSQL)
		if execErr != nil {
			if isAlreadyExists(execErr) {
				slog.Info("migration already applied (objects exist), recording", "file", file)
			} else {
				return fmt.Errorf("running migration %s: %w", file, execErr)
			}
		} else {
			slog.Info("applied migration", "file", file)
		}

		_, err = pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", file)
		if err != nil {
			return fmt.Errorf("recording migration %s: %w", file, err)
		}
	}

	return nil
}

// extractGooseUp returns the SQL between "-- +goose Up" and "-- +goose Down".
func extractGooseUp(content string) string {
	upIdx := strings.Index(content, "-- +goose Up")
	if upIdx == -1 {
		return ""
	}
	sql := content[upIdx+len("-- +goose Up"):]

	downIdx := strings.Index(sql, "-- +goose Down")
	if downIdx != -1 {
		sql = sql[:downIdx]
	}

	return strings.TrimSpace(sql)
}
