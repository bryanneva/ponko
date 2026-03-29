-- +goose Up
ALTER TABLE thread_messages ADD COLUMN user_id TEXT;

-- +goose Down
ALTER TABLE thread_messages DROP COLUMN user_id;
