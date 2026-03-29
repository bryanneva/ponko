-- +goose Up
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS total_tasks INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS completed_tasks INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE workflows DROP COLUMN IF EXISTS completed_tasks;
ALTER TABLE workflows DROP COLUMN IF EXISTS total_tasks;
