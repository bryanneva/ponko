package queue

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

const defaultWorkerConcurrency = 1

// New creates a River client connected to the given pool.
// It runs River's migrations on startup and configures the worker pool
// with concurrency from WORKER_CONCURRENCY env var (default 10 for the Slack bot).
func New(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers, periodicJobs []*river.PeriodicJob) (*river.Client[pgx.Tx], error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("creating river migrator: %w", err)
	}

	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("running river migrations: %w", err)
	}
	slog.Info("river migrations complete")

	concurrency := 10
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		c, parseErr := strconv.Atoi(v)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing WORKER_CONCURRENCY: %w", parseErr)
		}
		concurrency = c
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		PeriodicJobs: periodicJobs,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: concurrency},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("creating river client: %w", err)
	}

	return client, nil
}

// RunMigrations runs River's schema migrations against the pool.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) (*rivermigrate.MigrateResult, error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("creating river migrator: %w", err)
	}

	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("running river migrations: %w", err)
	}

	return res, nil
}

// WorkerConcurrency reads the WORKER_CONCURRENCY env var, falling back to 1.
func WorkerConcurrency() int {
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultWorkerConcurrency
}

// NewWithConcurrency creates and starts a River client with explicit concurrency.
func NewWithConcurrency(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers, concurrency int) (*river.Client[pgx.Tx], error) {
	if concurrency <= 0 {
		concurrency = defaultWorkerConcurrency
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: concurrency},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating river client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting river client: %w", err)
	}

	return client, nil
}
