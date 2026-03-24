-- +goose Up

-- Add signing secret to webhooks for HMAC-SHA256 payload verification
ALTER TABLE webhook_config ADD COLUMN secret TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN; this is a no-op for down migration
