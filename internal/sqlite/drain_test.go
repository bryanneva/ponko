package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"

	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/worker"
)

func TestDrainRiverQueueWaitsForRunnableJobs(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlite.MigrateRiver(ctx, db); err != nil {
		t.Fatalf("MigrateRiver: %v", err)
	}

	workers := river.NewWorkers()
	worker.RegisterPlaceholder(workers)
	client, err := river.NewClient(riversqlite.New(db), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = client.Stop(context.Background()) })

	if _, err := client.Insert(ctx, worker.PlaceholderArgs{}, nil); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := sqlite.DrainRiverQueue(ctx, db); err != nil {
		t.Fatalf("DrainRiverQueue: %v", err)
	}
}
