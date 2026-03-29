package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/workflow"
)

type RespondWorker struct {
	river.WorkerDefaults[RespondArgs]
	Pool *pgxpool.Pool
}

func (w *RespondWorker) Work(ctx context.Context, job *river.Job[RespondArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "respond", args.WorkflowID, args.TraceContext)
	defer span.End()

	step := args.ResponseStep
	if step == "" {
		step = "process"
	}
	data, err := workflow.GetOutput(ctx, w.Pool, args.WorkflowID, step)
	if err != nil {
		return fmt.Errorf("getting %s output: %w", step, err)
	}

	var payload struct {
		Response    string `json:"response"`
		Channel     string `json:"channel"`
		ThreadTS    string `json:"thread_ts"`
		EventTS     string `json:"event_ts"`
		ChannelType string `json:"channel_type"`
	}
	if unmarshalErr := json.Unmarshal(data, &payload); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling %s output: %w", step, unmarshalErr)
	}

	stepID, err := workflow.CreateStep(ctx, w.Pool, args.WorkflowID, "respond")
	if err != nil {
		return fmt.Errorf("creating respond step: %w", err)
	}

	err = workflow.UpdateStepStatus(ctx, w.Pool, stepID, "complete")
	if err != nil {
		return fmt.Errorf("updating respond step status: %w", err)
	}

	err = workflow.SaveOutput(ctx, w.Pool, args.WorkflowID, "respond", data)
	if err != nil {
		return fmt.Errorf("saving respond output: %w", err)
	}

	err = workflow.UpdateWorkflowStatus(ctx, w.Pool, args.WorkflowID, "completed")
	if err != nil {
		return fmt.Errorf("updating workflow status: %w", err)
	}
	appOtel.RecordWorkflowCompleted(ctx)

	conversationID, err := workflow.GetConversationID(ctx, w.Pool, args.WorkflowID)
	if err != nil {
		return fmt.Errorf("getting conversation_id: %w", err)
	}

	if conversationID != "" {
		contentJSON, marshalErr := json.Marshal(payload.Response)
		if marshalErr != nil {
			return fmt.Errorf("marshaling outbox content: %w", marshalErr)
		}
		_, enqueueErr := saga.Enqueue(ctx, w.Pool, saga.OutboxEntry{
			ConversationID: conversationID,
			WorkflowID:     args.WorkflowID,
			ChannelID:      payload.Channel,
			ThreadTS:       payload.ThreadTS,
			MessageType:    "text",
			Content:        contentJSON,
		})
		if enqueueErr != nil {
			return fmt.Errorf("enqueuing outbox entry: %w", enqueueErr)
		}

		if transErr := saga.TransitionStatus(ctx, w.Pool, conversationID, "active", "completed"); transErr != nil {
			slog.Warn("could not transition conversation to completed (may already be completed)",
				"conversation_id", conversationID, "workflow_id", args.WorkflowID, "error", transErr)
		}

		return nil
	}

	riverClient := river.ClientFromContext[pgx.Tx](ctx)
	_, insertErr := riverClient.Insert(ctx, SlackReplyArgs{
		WorkflowID:   args.WorkflowID,
		Response:     payload.Response,
		Channel:      payload.Channel,
		ThreadTS:     payload.ThreadTS,
		EventTS:      payload.EventTS,
		ChannelType:  payload.ChannelType,
		TraceContext: appOtel.InjectTraceContext(ctx),
	}, nil)
	if insertErr != nil {
		return fmt.Errorf("enqueuing slack reply job: %w", insertErr)
	}

	return nil
}
