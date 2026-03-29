package saga

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxDeliveryAttempts = 5

type OutboxEntry struct {
	CreatedAt      time.Time       `json:"created_at"`
	LastAttemptAt  *time.Time      `json:"last_attempt_at"`
	DeliveredAt    *time.Time      `json:"delivered_at"`
	SlackMessageTS *string         `json:"slack_message_ts"`
	OutboxID       string          `json:"outbox_id"`
	ConversationID string          `json:"conversation_id"`
	WorkflowID     string          `json:"workflow_id"`
	ChannelID      string          `json:"channel_id"`
	ThreadTS       string          `json:"thread_ts"`
	MessageType    string          `json:"message_type"`
	Status         string          `json:"status"`
	Content        json.RawMessage `json:"content"`
	Attempts       int             `json:"attempts"`
}

func Enqueue(ctx context.Context, pool *pgxpool.Pool, entry OutboxEntry) (string, error) {
	var outboxID string
	err := pool.QueryRow(ctx,
		`INSERT INTO outbox (conversation_id, workflow_id, channel_id, thread_ts, message_type, content)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING outbox_id`,
		nilIfEmpty(entry.ConversationID), entry.WorkflowID, entry.ChannelID, entry.ThreadTS, entry.MessageType, entry.Content,
	).Scan(&outboxID)
	if err != nil {
		return "", fmt.Errorf("enqueuing outbox entry: %w", err)
	}
	return outboxID, nil
}

func ClaimPending(ctx context.Context, pool *pgxpool.Pool, batchSize int) ([]OutboxEntry, error) {
	rows, err := pool.Query(ctx,
		`UPDATE outbox SET status = 'delivering', last_attempt_at = now(), attempts = attempts + 1
		WHERE outbox_id IN (
			SELECT outbox_id FROM outbox
			WHERE status = 'pending'
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING created_at, last_attempt_at, delivered_at, slack_message_ts, outbox_id,
			COALESCE(conversation_id::text, ''), workflow_id, channel_id,
			COALESCE(thread_ts, ''), message_type, status, content, attempts`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("claiming pending outbox entries: %w", err)
	}
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OutboxEntry])
	if err != nil {
		return nil, fmt.Errorf("scanning claimed outbox entries: %w", err)
	}
	return entries, nil
}

func MarkDelivered(ctx context.Context, pool *pgxpool.Pool, outboxID string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE outbox SET status = 'delivered', delivered_at = now() WHERE outbox_id = $1`,
		outboxID,
	)
	if err != nil {
		return fmt.Errorf("marking outbox entry delivered: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("outbox entry %s not found", outboxID)
	}
	return nil
}

func MarkFailed(ctx context.Context, pool *pgxpool.Pool, outboxID string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE outbox SET status = 'failed' WHERE outbox_id = $1`,
		outboxID,
	)
	if err != nil {
		return fmt.Errorf("marking outbox entry failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("outbox entry %s not found", outboxID)
	}
	return nil
}

func ResetForRetry(ctx context.Context, pool *pgxpool.Pool, outboxID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE outbox SET status = 'pending' WHERE outbox_id = $1`,
		outboxID,
	)
	if err != nil {
		return fmt.Errorf("resetting outbox entry for retry: %w", err)
	}
	return nil
}

func GetOutboxEntry(ctx context.Context, pool *pgxpool.Pool, outboxID string) (*OutboxEntry, error) {
	var e OutboxEntry
	err := pool.QueryRow(ctx,
		`SELECT created_at, last_attempt_at, delivered_at, slack_message_ts, outbox_id,
			COALESCE(conversation_id::text, ''), workflow_id, channel_id,
			COALESCE(thread_ts, ''), message_type, status, content, attempts
		FROM outbox WHERE outbox_id = $1`,
		outboxID,
	).Scan(&e.CreatedAt, &e.LastAttemptAt, &e.DeliveredAt, &e.SlackMessageTS, &e.OutboxID,
		&e.ConversationID, &e.WorkflowID, &e.ChannelID,
		&e.ThreadTS, &e.MessageType, &e.Status, &e.Content, &e.Attempts)
	if err != nil {
		return nil, fmt.Errorf("getting outbox entry: %w", err)
	}
	return &e, nil
}

func SetSlackMessageTS(ctx context.Context, pool *pgxpool.Pool, outboxID, ts string) error {
	_, err := pool.Exec(ctx,
		`UPDATE outbox SET slack_message_ts = $1 WHERE outbox_id = $2`,
		ts, outboxID,
	)
	if err != nil {
		return fmt.Errorf("setting slack_message_ts: %w", err)
	}
	return nil
}

func ListOutboxByConversation(ctx context.Context, pool *pgxpool.Pool, conversationID string) ([]OutboxEntry, error) {
	rows, err := pool.Query(ctx,
		`SELECT created_at, last_attempt_at, delivered_at, slack_message_ts, outbox_id,
			COALESCE(conversation_id::text, ''), workflow_id, channel_id,
			COALESCE(thread_ts, ''), message_type, status, content, attempts
		FROM outbox WHERE conversation_id = $1 ORDER BY created_at`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing outbox entries for conversation: %w", err)
	}
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OutboxEntry])
	if err != nil {
		return nil, fmt.Errorf("scanning outbox entries for conversation: %w", err)
	}
	return entries, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
