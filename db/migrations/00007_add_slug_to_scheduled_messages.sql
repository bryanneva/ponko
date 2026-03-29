-- +goose Up
ALTER TABLE scheduled_messages ADD COLUMN slug TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_scheduled_messages_channel_slug ON scheduled_messages (channel_id, slug) WHERE slug != '';

-- +goose Down
DROP INDEX IF EXISTS idx_scheduled_messages_channel_slug;
ALTER TABLE scheduled_messages DROP COLUMN slug;
