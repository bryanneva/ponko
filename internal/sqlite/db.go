// Package sqlite provides SQLite connection helpers and schema migrations for ponko-runner.
package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register sqlite driver
)

// Open opens a SQLite database at path with WAL mode, a 5-second busy timeout,
// and a connection pool limited to 1 (SQLite doesn't support concurrent writers).
// Use ":memory:" for an in-memory database.
func Open(path string) (*sql.DB, error) {
	// Append pragmas for WAL mode and busy timeout via DSN
	var dsn string
	if path != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	} else {
		dsn = "file::memory:?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

// EnsureDir creates the parent directory of path if it doesn't exist.
func EnsureDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return nil
}
