-- +goose Up
ALTER TABLE users ADD COLUMN password_changed_at DATETIME;

-- +goose Down
-- SQLite does not support DROP COLUMN before 3.35.0; no-op for safety.
