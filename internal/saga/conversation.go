package saga

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type Conversation struct {
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ConversationID string    `json:"conversation_id"`
	ChannelID      string    `json:"channel_id"`
	ThreadTS       string    `json:"thread_ts"`
	Status         string    `json:"status"`
	Turns          []Turn    `json:"turns"`
}

type Turn struct {
	CreatedAt      time.Time `json:"created_at"`
	TurnID         string    `json:"turn_id"`
	ConversationID string    `json:"conversation_id"`
	WorkflowID     string    `json:"workflow_id"`
	TriggerType    string    `json:"trigger_type"`
	TurnIndex      int       `json:"turn_index"`
}

func FindOrCreateConversation(ctx context.Context, pool *pgxpool.Pool, channelID, threadTS string) (string, bool, error) {
	var conversationID string
	var inserted bool

	err := pool.QueryRow(ctx,
		`WITH ins AS (
			INSERT INTO conversations (channel_id, thread_ts)
			VALUES ($1, $2)
			ON CONFLICT (channel_id, thread_ts) DO NOTHING
			RETURNING conversation_id, true AS inserted
		)
		SELECT conversation_id, inserted FROM ins
		UNION ALL
		SELECT conversation_id, false AS inserted FROM conversations
		WHERE channel_id = $1 AND thread_ts = $2
		  AND NOT EXISTS (SELECT 1 FROM ins)
		LIMIT 1`,
		channelID, threadTS,
	).Scan(&conversationID, &inserted)
	if err != nil {
		return "", false, fmt.Errorf("finding or creating conversation: %w", err)
	}

	return conversationID, inserted, nil
}

// AddTurn appends a turn to a conversation. Uses a retry loop to handle
// concurrent inserts that collide on the UNIQUE(conversation_id, turn_index) constraint.
func AddTurn(ctx context.Context, pool *pgxpool.Pool, conversationID, workflowID, triggerType string) error {
	const maxRetries = 3
	for i := range maxRetries {
		_, err := pool.Exec(ctx,
			`INSERT INTO conversation_turns (conversation_id, workflow_id, trigger_type, turn_index)
			VALUES ($1, $2, $3, (SELECT COALESCE(MAX(turn_index), -1) + 1 FROM conversation_turns WHERE conversation_id = $1))`,
			conversationID, workflowID, triggerType,
		)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 && isUniqueViolation(err) {
			continue
		}
		return fmt.Errorf("adding turn: %w", err)
	}
	return fmt.Errorf("adding turn: exhausted retries for conversation %s", conversationID)
}

func TransitionStatus(ctx context.Context, pool *pgxpool.Pool, conversationID, fromStatus, toStatus string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE conversations SET status = $1, updated_at = now() WHERE conversation_id = $2 AND status = $3`,
		toStatus, conversationID, fromStatus,
	)
	if err != nil {
		return fmt.Errorf("transitioning conversation status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("conversation %s is not in status %q", conversationID, fromStatus)
	}
	return nil
}

func GetConversation(ctx context.Context, pool *pgxpool.Pool, conversationID string) (*Conversation, error) {
	c := &Conversation{}
	err := pool.QueryRow(ctx,
		`SELECT conversation_id, channel_id, thread_ts, status, created_at, updated_at
		FROM conversations WHERE conversation_id = $1`,
		conversationID,
	).Scan(&c.ConversationID, &c.ChannelID, &c.ThreadTS, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting conversation: %w", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT created_at, turn_id, conversation_id, workflow_id, trigger_type, turn_index
		FROM conversation_turns WHERE conversation_id = $1 ORDER BY turn_index`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying conversation turns: %w", err)
	}
	c.Turns, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Turn])
	if err != nil {
		return nil, fmt.Errorf("scanning conversation turns: %w", err)
	}

	return c, nil
}

type ConversationSummary struct {
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ConversationID string    `json:"conversation_id"`
	ChannelID      string    `json:"channel_id"`
	Status         string    `json:"status"`
	TurnCount      int       `json:"turn_count"`
}

func ListRecentConversations(ctx context.Context, pool *pgxpool.Pool, limit int) ([]ConversationSummary, error) {
	rows, err := pool.Query(ctx,
		`SELECT c.created_at, c.updated_at, c.conversation_id, c.channel_id, c.status,
			COUNT(ct.turn_id)::int AS turn_count
		FROM conversations c
		LEFT JOIN conversation_turns ct ON c.conversation_id = ct.conversation_id
		GROUP BY c.conversation_id
		ORDER BY c.updated_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing recent conversations: %w", err)
	}
	convs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ConversationSummary])
	if err != nil {
		return nil, fmt.Errorf("scanning recent conversations: %w", err)
	}
	return convs, nil
}
