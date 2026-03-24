-- +goose Up
CREATE TABLE email_verification_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    token TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_email_verify_tokens_token ON email_verification_tokens(token);
CREATE INDEX idx_email_verify_tokens_user_id ON email_verification_tokens(user_id);

-- +goose Down
DROP TABLE IF EXISTS email_verification_tokens;
