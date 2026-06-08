-- +goose Up

-- V2-429: dead-letter queue for webhook deliveries that exhaust every retry.
-- The auth-notifier path (password reset / email verification links) shares the
-- webhook delivery pipeline, so a transient receiver failure would otherwise
-- silently drop a user's recovery link after ~6s with only a log line. Each row
-- captures the full payload so an operator can re-drive the delivery.
-- NOTE: payload may contain sensitive one-time links (auth events) — this table
-- is admin-only and resolved rows are pruned on the log-retention schedule.
CREATE TABLE webhook_dead_letter (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id INTEGER NOT NULL REFERENCES webhook_config(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,            -- JSON-encoded WebhookPayload, re-formatted at resend
    last_status_code INTEGER,
    last_error TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    resend_count INTEGER NOT NULL DEFAULT 0,
    is_auth INTEGER NOT NULL DEFAULT 0, -- auth.* event (recovery link) — escalated to system_log
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at DATETIME              -- set when re-driven OK or manually dismissed
);

CREATE INDEX idx_webhook_dead_letter_webhook_id ON webhook_dead_letter(webhook_id);
CREATE INDEX idx_webhook_dead_letter_resolved_at ON webhook_dead_letter(resolved_at);

-- +goose Down

DROP TABLE webhook_dead_letter;
