package conversation

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Message represents a conversation message stored in the thread_messages table.
type Message struct {
	CreatedAt time.Time
	Role      string
	Content   string
	UserID    string
}

// ThreadKey computes the canonical thread key from a channel and thread timestamp.
func ThreadKey(channel, threadTS string) string {
	return channel + ":" + threadTS
}

// SaveThreadMessage inserts a message into the thread_messages table.
func SaveThreadMessage(ctx context.Context, pool *pgxpool.Pool, threadKey, role, content, userID string) error {
	_, err := pool.Exec(ctx,
		"INSERT INTO thread_messages (thread_key, role, content, user_id) VALUES ($1, $2, $3, $4)",
		threadKey, role, content, userID,
	)
	if err != nil {
		return fmt.Errorf("saving thread message: %w", err)
	}
	return nil
}

// SaveThreadMessages inserts a user and assistant message pair in a single query.
func SaveThreadMessages(ctx context.Context, pool *pgxpool.Pool, threadKey, userRole, userContent, assistantRole, assistantContent, userID string) error {
	_, err := pool.Exec(ctx,
		"INSERT INTO thread_messages (thread_key, role, content, user_id) VALUES ($1, $2, $3, $6), ($1, $4, $5, '')",
		threadKey, userRole, userContent, assistantRole, assistantContent, userID,
	)
	if err != nil {
		return fmt.Errorf("saving thread messages: %w", err)
	}
	return nil
}

// Participation holds the result of checking the bot's thread participation.
type Participation struct {
	Any    bool
	Recent bool
}

// CheckParticipation checks both whether the bot has ever participated in a thread
// and whether it has participated recently (within maxAge), in a single query.
func CheckParticipation(ctx context.Context, pool *pgxpool.Pool, threadKey string, maxAge time.Duration) (Participation, error) {
	cutoff := time.Now().Add(-maxAge)
	var p Participation
	err := pool.QueryRow(ctx,
		`SELECT
			EXISTS(SELECT 1 FROM thread_messages WHERE thread_key = $1 AND role = 'assistant') AS any_participation,
			EXISTS(SELECT 1 FROM thread_messages WHERE thread_key = $1 AND role = 'assistant' AND created_at > $2) AS recent_participation`,
		threadKey, cutoff,
	).Scan(&p.Any, &p.Recent)
	if err != nil {
		return Participation{}, fmt.Errorf("checking participation: %w", err)
	}
	return p, nil
}

// GetThreadMessages retrieves the most recent messages for a thread, returned in chronological order.
func GetThreadMessages(ctx context.Context, pool *pgxpool.Pool, threadKey string, limit int) ([]Message, error) {
	rows, err := pool.Query(ctx,
		"SELECT created_at, role, content, COALESCE(user_id, '') FROM (SELECT created_at, role, content, user_id FROM thread_messages WHERE thread_key = $1 ORDER BY created_at DESC LIMIT $2) sub ORDER BY created_at ASC",
		threadKey, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying thread messages: %w", err)
	}

	messages, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Message])
	if err != nil {
		return nil, fmt.Errorf("scanning thread messages: %w", err)
	}

	return messages, nil
}
