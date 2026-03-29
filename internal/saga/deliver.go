package saga

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/slack"
)

type SlackDeliverer interface {
	PostMessage(ctx context.Context, channel, text, threadTS string) error
	PostBlocks(ctx context.Context, channel, text string, blocks []slack.Block, threadTS string) (string, error)
}

type OutboxDeliverArgs struct{}

func (OutboxDeliverArgs) Kind() string { return "outbox_deliver" }

type OutboxDeliverWorker struct {
	river.WorkerDefaults[OutboxDeliverArgs]
	Pool  *pgxpool.Pool
	Slack SlackDeliverer
}

func (w *OutboxDeliverWorker) Work(ctx context.Context, _ *river.Job[OutboxDeliverArgs]) error {
	return w.DeliverPending(ctx)
}

func (w *OutboxDeliverWorker) DeliverPending(ctx context.Context) error {
	entries, err := ClaimPending(ctx, w.Pool, 10)
	if err != nil {
		return fmt.Errorf("claiming pending entries: %w", err)
	}

	for _, entry := range entries {
		w.deliver(ctx, entry)
	}
	return nil
}

func (w *OutboxDeliverWorker) deliver(ctx context.Context, entry OutboxEntry) {
	var deliverErr error

	switch entry.MessageType {
	case "blocks":
		var rawBlocks []json.RawMessage
		if err := json.Unmarshal(entry.Content, &rawBlocks); err != nil {
			w.handleFailure(ctx, entry, fmt.Errorf("unmarshaling block content: %w", err))
			return
		}

		var blockSlice []slack.Block
		for _, rb := range rawBlocks {
			blockSlice = append(blockSlice, slack.RawBlock(rb))
		}

		var ts string
		ts, deliverErr = w.Slack.PostBlocks(ctx, entry.ChannelID, "New message", blockSlice, entry.ThreadTS)
		if deliverErr == nil && ts != "" {
			if setErr := SetSlackMessageTS(ctx, w.Pool, entry.OutboxID, ts); setErr != nil {
				slog.Error("failed to store slack_message_ts", "outbox_id", entry.OutboxID, "error", setErr)
			}
		}
	default:
		var text string
		if err := json.Unmarshal(entry.Content, &text); err != nil {
			text = string(entry.Content)
		}
		text = slack.MarkdownToMrkdwn(text)
		deliverErr = w.Slack.PostMessage(ctx, entry.ChannelID, text, entry.ThreadTS)
	}

	if deliverErr != nil {
		w.handleFailure(ctx, entry, deliverErr)
		return
	}

	if err := MarkDelivered(ctx, w.Pool, entry.OutboxID); err != nil {
		slog.Error("failed to mark outbox entry delivered", "outbox_id", entry.OutboxID, "error", err)
	}
}

func (w *OutboxDeliverWorker) handleFailure(ctx context.Context, entry OutboxEntry, deliverErr error) {
	if slack.IsPermanentError(deliverErr) {
		slog.Warn("permanent Slack error, marking outbox entry failed",
			"outbox_id", entry.OutboxID,
			"error", deliverErr,
		)
		if err := MarkFailed(ctx, w.Pool, entry.OutboxID); err != nil {
			slog.Error("failed to mark outbox entry failed", "outbox_id", entry.OutboxID, "error", err)
		}
		return
	}

	if entry.Attempts >= MaxDeliveryAttempts {
		slog.Warn("max delivery attempts reached, marking outbox entry failed",
			"outbox_id", entry.OutboxID,
			"attempts", entry.Attempts,
		)
		if err := MarkFailed(ctx, w.Pool, entry.OutboxID); err != nil {
			slog.Error("failed to mark outbox entry failed", "outbox_id", entry.OutboxID, "error", err)
		}
		return
	}

	slog.Warn("delivery failed, resetting for retry",
		"outbox_id", entry.OutboxID,
		"attempts", entry.Attempts,
		"error", deliverErr,
	)
	if err := ResetForRetry(ctx, w.Pool, entry.OutboxID); err != nil {
		slog.Error("failed to reset outbox entry for retry", "outbox_id", entry.OutboxID, "error", err)
	}
}
