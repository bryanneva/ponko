package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultDSN        = "postgres://agent:agent@localhost:5433/agent_dev?sslmode=disable"
	healthCheckPeriod = 30 * time.Second
	maxConnIdleTime   = 5 * time.Minute
	maxConnLifetime   = 30 * time.Minute
)

// NewPool creates a configured pgxpool.Pool. If dsn is empty, it falls back
// to DATABASE_URL env var, then to the default local development DSN.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = defaultDSN
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}

	config.HealthCheckPeriod = healthCheckPeriod
	config.MaxConnIdleTime = maxConnIdleTime
	config.MaxConnLifetime = maxConnLifetime

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return pool, nil
}
