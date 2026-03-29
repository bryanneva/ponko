package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Message struct {
	NextRunAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastRunAt    *time.Time
	ScheduleCron *string
	CreatedBy    *string
	ID           string
	ChannelID    string
	Prompt       string
	Slug         string
	OneShot      bool
	Enabled      bool
}

const maxSlugAttempts = 10

var ErrQuotaExceeded = errors.New("schedule quota exceeded")

func CreateWithQuota(ctx context.Context, pool *pgxpool.Pool, msg Message, maxPerChannel int) error {
	slug := msg.Slug
	for attempt := range maxSlugAttempts {
		if attempt > 0 {
			slug = fmt.Sprintf("%s-%d", msg.Slug, attempt+1)
		}
		var insertedID string
		err := pool.QueryRow(ctx,
			`WITH quota AS (
				SELECT COUNT(*) AS cnt FROM scheduled_messages WHERE channel_id = $1 AND enabled = true
			)
			INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, created_by, slug)
			SELECT $1, $2, $3, $4, $5, $6, $7
			FROM quota WHERE quota.cnt < $8
			RETURNING id`,
			msg.ChannelID, msg.Prompt, msg.ScheduleCron, msg.NextRunAt, msg.OneShot, msg.CreatedBy, slug, maxPerChannel,
		).Scan(&insertedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrQuotaExceeded
			}
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				continue
			}
			return fmt.Errorf("creating scheduled message: %w", err)
		}
		return nil
	}
	return fmt.Errorf("slug collision: exhausted %d attempts for slug %q", maxSlugAttempts, msg.Slug)
}

func Create(ctx context.Context, pool *pgxpool.Pool, msg Message) error {
	slug := msg.Slug
	for attempt := range maxSlugAttempts {
		if attempt > 0 {
			slug = fmt.Sprintf("%s-%d", msg.Slug, attempt+1)
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, created_by, slug)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			msg.ChannelID, msg.Prompt, msg.ScheduleCron, msg.NextRunAt, msg.OneShot, msg.CreatedBy, slug,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				continue
			}
			return fmt.Errorf("creating scheduled message: %w", err)
		}
		return nil
	}
	return fmt.Errorf("slug collision: exhausted %d attempts for slug %q", maxSlugAttempts, msg.Slug)
}

func GetDue(ctx context.Context, pool *pgxpool.Pool) ([]Message, error) {
	rows, err := pool.Query(ctx,
		`SELECT next_run_at, created_at, updated_at, last_run_at, schedule_cron, created_by, id, channel_id, prompt, slug, one_shot, enabled
		FROM scheduled_messages
		WHERE enabled = true AND next_run_at <= now()
		ORDER BY next_run_at ASC
		LIMIT 100`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying due scheduled messages: %w", err)
	}

	messages, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Message])
	if err != nil {
		return nil, fmt.Errorf("scanning due scheduled messages: %w", err)
	}
	return messages, nil
}

func MarkRun(ctx context.Context, pool *pgxpool.Pool, id string, nextRunAt *time.Time) error {
	if nextRunAt != nil {
		_, err := pool.Exec(ctx,
			`UPDATE scheduled_messages SET last_run_at = now(), next_run_at = $2, updated_at = now() WHERE id = $1`,
			id, *nextRunAt,
		)
		if err != nil {
			return fmt.Errorf("marking scheduled message run (recurring): %w", err)
		}
		return nil
	}
	_, err := pool.Exec(ctx,
		`UPDATE scheduled_messages SET last_run_at = now(), enabled = false, updated_at = now() WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("marking scheduled message run (one-shot): %w", err)
	}
	return nil
}

func Cancel(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx,
		`UPDATE scheduled_messages SET enabled = false, updated_at = now() WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("cancelling scheduled message: %w", err)
	}
	return nil
}

func GetBySlug(ctx context.Context, pool *pgxpool.Pool, channelID, slug string) (*Message, error) {
	rows, err := pool.Query(ctx,
		`SELECT next_run_at, created_at, updated_at, last_run_at, schedule_cron, created_by, id, channel_id, prompt, slug, one_shot, enabled
		FROM scheduled_messages
		WHERE channel_id = $1 AND slug = $2 AND enabled = true`,
		channelID, slug,
	)
	if err != nil {
		return nil, fmt.Errorf("querying schedule by slug: %w", err)
	}

	msg, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Message])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no active schedule with slug %q found in this channel", slug)
		}
		return nil, fmt.Errorf("scanning schedule by slug: %w", err)
	}
	return &msg, nil
}

func ListByChannel(ctx context.Context, pool *pgxpool.Pool, channelID string) ([]Message, error) {
	rows, err := pool.Query(ctx,
		`SELECT next_run_at, created_at, updated_at, last_run_at, schedule_cron, created_by, id, channel_id, prompt, slug, one_shot, enabled
		FROM scheduled_messages
		WHERE enabled = true AND channel_id = $1
		ORDER BY next_run_at ASC`,
		channelID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled messages for channel: %w", err)
	}

	messages, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Message])
	if err != nil {
		return nil, fmt.Errorf("scanning scheduled messages for channel: %w", err)
	}
	return messages, nil
}
