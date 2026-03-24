-- +goose Up
CREATE TABLE email_verification_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    token TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_email_verify_tokens_token ON email_verification_tokens(token);
CREATE INDEX idx_email_verify_tokens_user_id ON email_verification_tokens(user_id);

-- +goose Down
DROP TABLE IF EXISTS email_verification_tokens;
