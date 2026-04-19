package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bryanneva/ponko/internal/sqlite"
)

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// openDB opens the SQLite database at the given path (after expanding ~).
func openDB(path string) (*sql.DB, error) {
	db, err := sqlite.Open(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return db, nil
}
