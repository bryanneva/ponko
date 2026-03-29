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

// New creates a River client connected to the given pool.
// It runs River's migrations on startup and configures the worker pool
// with concurrency from WORKER_CONCURRENCY env var (default 10).
func New(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers, periodicJobs []*river.PeriodicJob) (*river.Client[pgx.Tx], error) {
	// Run River migrations
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
