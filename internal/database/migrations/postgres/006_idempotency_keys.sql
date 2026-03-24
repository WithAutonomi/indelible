-- +goose Up

-- Idempotency keys for safe POST retries (e.g. uploads)
CREATE TABLE idempotency_keys (
    key TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    status_code INTEGER NOT NULL,
    response_body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (key, user_id)
);

CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys(created_at);

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
