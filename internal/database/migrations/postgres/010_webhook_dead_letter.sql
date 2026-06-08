-- +goose Up

-- See sqlite/010_webhook_dead_letter.sql for rationale (V2-429 webhook DLQ).
CREATE TABLE webhook_dead_letter (
    id BIGSERIAL PRIMARY KEY,
    webhook_id BIGINT NOT NULL REFERENCES webhook_config(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    last_status_code INTEGER,
    last_error TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    resend_count INTEGER NOT NULL DEFAULT 0,
    is_auth BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_webhook_dead_letter_webhook_id ON webhook_dead_letter(webhook_id);
CREATE INDEX idx_webhook_dead_letter_resolved_at ON webhook_dead_letter(resolved_at);

-- +goose Down

DROP TABLE webhook_dead_letter;
