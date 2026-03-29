package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/workflow"
)

type ReceiveArgs struct {
	WorkflowID   string `json:"workflow_id"`
	ThreadKey    string `json:"thread_key"`
	Channel      string `json:"channel"`
	ThreadTS     string `json:"thread_ts"`
	EventTS      string `json:"event_ts"`
	ChannelType  string `json:"channel_type"`
	Message      string `json:"message"`
	UserID       string `json:"user_id"`
	TraceContext string `json:"trace_context,omitempty"`
}

func (ReceiveArgs) Kind() string { return "receive" }

type ReceiveWorker struct {
	river.WorkerDefaults[ReceiveArgs]
	Pool *pgxpool.Pool
}

func (w *ReceiveWorker) Work(ctx context.Context, job *river.Job[ReceiveArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "receive", args.WorkflowID, args.TraceContext)
	defer span.End()

	stepID, err := workflow.CreateStep(ctx, w.Pool, args.WorkflowID, "receive")
	if err != nil {
		return fmt.Errorf("creating receive step: %w", err)
	}

	err = workflow.UpdateStepStatus(ctx, w.Pool, stepID, "complete")
	if err != nil {
		return fmt.Errorf("updating receive step status: %w", err)
	}

	data, err := json.Marshal(map[string]string{
		"message":      args.Message,
		"thread_key":   args.ThreadKey,
		"channel":      args.Channel,
		"thread_ts":    args.ThreadTS,
		"event_ts":     args.EventTS,
		"channel_type": args.ChannelType,
		"user_id":      args.UserID,
	})
	if err != nil {
		return fmt.Errorf("marshaling receive output: %w", err)
	}

	err = workflow.SaveOutput(ctx, w.Pool, args.WorkflowID, "receive", data)
	if err != nil {
		return fmt.Errorf("saving receive output: %w", err)
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	_, err = client.Insert(ctx, PlanArgs{
		WorkflowID:   args.WorkflowID,
		TraceContext: appOtel.InjectTraceContext(ctx),
	}, nil)
	if err != nil {
		return fmt.Errorf("enqueuing plan job: %w", err)
	}

	return nil
}
