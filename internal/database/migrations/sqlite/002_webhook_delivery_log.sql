-- +goose Up
CREATE TABLE webhook_delivery_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id INTEGER NOT NULL REFERENCES webhook_config(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    status_code INTEGER,
    success INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 1,
    error_message TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_webhook_delivery_log_webhook_id ON webhook_delivery_log(webhook_id);
CREATE INDEX idx_webhook_delivery_log_created_at ON webhook_delivery_log(created_at);

-- +goose Down
DROP TABLE IF EXISTS webhook_delivery_log;
