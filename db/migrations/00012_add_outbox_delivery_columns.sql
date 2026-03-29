-- +goose Up
ALTER TABLE outbox ADD COLUMN IF NOT EXISTS slack_message_ts TEXT;
ALTER TABLE channel_configs ADD COLUMN IF NOT EXISTS approval_required BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE channel_configs DROP COLUMN IF EXISTS approval_required;
ALTER TABLE outbox DROP COLUMN IF EXISTS slack_message_ts;
