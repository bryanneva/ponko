package channel

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	RespondModeMentionOnly = "mention_only"
	RespondModeAllMessages = "all_messages"
)

type Config struct {
	ChannelID        string
	SystemPrompt     string
	RespondMode      string
	ToolAllowlist    []string // nil = all tools, empty = no tools
	ApprovalRequired bool
}

func (c *Config) EffectiveRespondMode() string {
	if c == nil || c.RespondMode == "" {
		return RespondModeMentionOnly
	}
	return c.RespondMode
}

type ConfigRow struct {
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ChannelID        string
	SystemPrompt     string
	RespondMode      string
	ToolAllowlist    []string
	ApprovalRequired bool
}

func ListAll(ctx context.Context, pool *pgxpool.Pool) ([]ConfigRow, error) {
	rows, err := pool.Query(ctx,
		"SELECT channel_id, system_prompt, respond_mode, tool_allowlist, approval_required, created_at, updated_at FROM channel_configs ORDER BY channel_id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []ConfigRow
	for rows.Next() {
		var c ConfigRow
		if err := rows.Scan(&c.ChannelID, &c.SystemPrompt, &c.RespondMode, &c.ToolAllowlist, &c.ApprovalRequired, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func GetConfig(ctx context.Context, pool *pgxpool.Pool, channelID string) (*Config, error) {
	var cfg Config
	err := pool.QueryRow(ctx,
		"SELECT channel_id, system_prompt, respond_mode, tool_allowlist, approval_required FROM channel_configs WHERE channel_id = $1",
		channelID,
	).Scan(&cfg.ChannelID, &cfg.SystemPrompt, &cfg.RespondMode, &cfg.ToolAllowlist, &cfg.ApprovalRequired)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func UpsertConfig(ctx context.Context, pool *pgxpool.Pool, cfg *Config) error {
	respondMode := cfg.EffectiveRespondMode()
	_, err := pool.Exec(ctx,
		`INSERT INTO channel_configs (channel_id, system_prompt, respond_mode, tool_allowlist, approval_required)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (channel_id) DO UPDATE SET
			system_prompt = EXCLUDED.system_prompt,
			respond_mode = EXCLUDED.respond_mode,
			tool_allowlist = EXCLUDED.tool_allowlist,
			approval_required = EXCLUDED.approval_required,
			updated_at = now()`,
		cfg.ChannelID, cfg.SystemPrompt, respondMode, cfg.ToolAllowlist, cfg.ApprovalRequired,
	)
	return err
}

func DeleteConfig(ctx context.Context, pool *pgxpool.Pool, channelID string) error {
	_, err := pool.Exec(ctx,
		"DELETE FROM channel_configs WHERE channel_id = $1",
		channelID,
	)
	return err
}
