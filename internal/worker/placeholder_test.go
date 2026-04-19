package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/queue"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/worker"
)

func TestPlaceholderWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	ctx := context.Background()
	pool := testutil.TestDB(t)

	// Run River migrations.
	_, err := queue.RunMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Register the placeholder worker.
	workers := river.NewWorkers()
	worker.RegisterPlaceholder(workers)

	// Create and start the River client.
	client, err := queue.NewWithConcurrency(ctx, pool, workers, 1)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Stop(ctx) })

	// Subscribe to completed jobs.
	events, cancel := client.Subscribe(river.EventKindJobCompleted)
	defer cancel()

	// Enqueue a placeholder job.
	_, err = client.Insert(ctx, worker.PlaceholderArgs{}, nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Wait for the job to complete.
	select {
	case evt := <-events:
		if evt.Kind != river.EventKindJobCompleted {
			t.Fatalf("unexpected event kind: %s", evt.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for placeholder job to complete")
	}
}
