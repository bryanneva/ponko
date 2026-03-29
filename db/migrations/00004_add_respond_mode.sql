-- +goose Up
ALTER TABLE channel_configs ADD COLUMN respond_mode TEXT NOT NULL DEFAULT 'mention_only';

-- +goose Down
ALTER TABLE channel_configs DROP COLUMN respond_mode;
