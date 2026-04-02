-- +goose Up
-- Composite index for DequeueNext: finds next queued upload not in backoff
CREATE INDEX IF NOT EXISTS idx_uploads_status_backoff ON uploads(status, backoff_until);

-- Index for RequeueStuck: finds uploads stuck in processing
CREATE INDEX IF NOT EXISTS idx_uploads_status_processing ON uploads(status, processing_at);

-- New settings for queue management
INSERT OR IGNORE INTO settings (key, value) VALUES ('max_queued_uploads', '500');
INSERT OR IGNORE INTO settings (key, value) VALUES ('upload_rate_limit_per_min', '60');

INSERT OR IGNORE INTO settings (key, value) VALUES ('wallet_balance_alert_threshold', '0');

-- Update default max_concurrent_uploads from 1 to 4
UPDATE settings SET value = '4' WHERE key = 'max_concurrent_uploads' AND value = '1';

-- +goose Down
DROP INDEX IF EXISTS idx_uploads_status_backoff;
DROP INDEX IF EXISTS idx_uploads_status_processing;
DELETE FROM settings WHERE key IN ('max_queued_uploads', 'upload_rate_limit_per_min');
