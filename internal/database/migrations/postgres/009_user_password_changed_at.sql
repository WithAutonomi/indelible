-- +goose Up
ALTER TABLE users ADD COLUMN password_changed_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN password_changed_at;
