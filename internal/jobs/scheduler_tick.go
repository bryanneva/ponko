package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/schedule"
)

type SchedulerTickArgs struct{}

func (SchedulerTickArgs) Kind() string { return "scheduler_tick" }

type SchedulerTickWorker struct {
	river.WorkerDefaults[SchedulerTickArgs]
	Pool *pgxpool.Pool
}

func (w *SchedulerTickWorker) Work(ctx context.Context, job *river.Job[SchedulerTickArgs]) error {
	due, err := schedule.GetDue(ctx, w.Pool)
	if err != nil {
		return fmt.Errorf("getting due scheduled messages: %w", err)
	}

	if len(due) == 0 {
		return nil
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, msg := range due {
		_, err := client.Insert(ctx, ProactiveMessageArgs{
			ScheduleID:   msg.ID,
			ChannelID:    msg.ChannelID,
			Prompt:       msg.Prompt,
			ScheduleCron: msg.ScheduleCron,
		}, nil)
		if err != nil {
			slog.Error("failed to enqueue proactive message", "schedule_id", msg.ID, "error", err)
			continue
		}
		slog.Info("enqueued proactive message", "schedule_id", msg.ID, "channel", msg.ChannelID)
	}

	return nil
}
