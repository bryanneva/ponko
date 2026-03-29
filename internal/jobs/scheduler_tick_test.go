package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"

	"github.com/bryanneva/ponko/internal/testutil"
)

func TestSchedulerTickWorker_Work(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	workers := river.NewWorkers()
	river.AddWorker(workers, &SchedulerTickWorker{Pool: pool})
	river.AddWorker(workers, &ProactiveMessageWorker{})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("creating river client: %v", err)
	}

	t.Run("empty due list is a no-op", func(t *testing.T) {
		worker := &SchedulerTickWorker{Pool: pool}
		job := &river.Job[SchedulerTickArgs]{}

		workCtx := rivertest.WorkContext(ctx, client)
		if workErr := worker.Work(workCtx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}
	})

	t.Run("enqueues proactive message for each due schedule", func(t *testing.T) {
		cron := "0 9 * * *"
		_, insertErr := pool.Exec(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			"C-TICK-1", "morning prompt", &cron, time.Now().Add(-1*time.Minute), false, true,
		)
		if insertErr != nil {
			t.Fatalf("inserting schedule 1: %v", insertErr)
		}
		_, insertErr = pool.Exec(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			"C-TICK-2", "evening prompt", &cron, time.Now().Add(-2*time.Minute), false, true,
		)
		if insertErr != nil {
			t.Fatalf("inserting schedule 2: %v", insertErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id IN ('C-TICK-1', 'C-TICK-2')")
		})

		worker := &SchedulerTickWorker{Pool: pool}
		job := &river.Job[SchedulerTickArgs]{}

		workCtx := rivertest.WorkContext(ctx, client)
		if workErr := worker.Work(workCtx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}

		var count int
		scanErr := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'proactive_message' AND args::text LIKE '%C-TICK-%'`,
		).Scan(&count)
		if scanErr != nil {
			t.Fatalf("querying river_job: %v", scanErr)
		}
		if count != 2 {
			t.Errorf("expected exactly 2 proactive_message jobs, got %d", count)
		}

		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM river_job WHERE kind = 'proactive_message' AND args::text LIKE '%C-TICK-%'")
		})
	})
}
