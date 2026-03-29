-- +goose Up
CREATE TABLE IF NOT EXISTS outbox (
    outbox_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID REFERENCES conversations(conversation_id),
    workflow_id     UUID NOT NULL REFERENCES workflows(workflow_id),
    channel_id      TEXT NOT NULL,
    thread_ts       TEXT,
    message_type    TEXT NOT NULL DEFAULT 'text',
    content         JSONB NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox (created_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS outbox;
