-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    slack_user_id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT '',
    cached_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS users;
