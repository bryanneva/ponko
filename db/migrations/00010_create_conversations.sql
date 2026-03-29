-- +goose Up
CREATE TABLE IF NOT EXISTS conversations (
    conversation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      TEXT NOT NULL,
    thread_ts       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_id, thread_ts)
);

CREATE INDEX IF NOT EXISTS idx_conversations_status ON conversations (status) WHERE status NOT IN ('completed', 'failed');

CREATE TABLE IF NOT EXISTS conversation_turns (
    turn_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(conversation_id),
    workflow_id     UUID NOT NULL REFERENCES workflows(workflow_id),
    turn_index      INTEGER NOT NULL,
    trigger_type    TEXT NOT NULL DEFAULT 'message',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, turn_index)
);

ALTER TABLE workflows ADD COLUMN IF NOT EXISTS conversation_id UUID REFERENCES conversations(conversation_id);

-- +goose Down
ALTER TABLE workflows DROP COLUMN IF EXISTS conversation_id;
DROP TABLE IF EXISTS conversation_turns;
DROP TABLE IF EXISTS conversations;
