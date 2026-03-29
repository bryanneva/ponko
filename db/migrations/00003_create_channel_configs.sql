-- +goose Up
CREATE TABLE channel_configs (
    channel_id TEXT PRIMARY KEY,
    system_prompt TEXT NOT NULL,
    tool_allowlist TEXT[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS channel_configs;
