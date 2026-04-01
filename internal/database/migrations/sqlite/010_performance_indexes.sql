-- +goose Up
-- Composite index for the most common query: user's uploads filtered by status
CREATE INDEX IF NOT EXISTS idx_uploads_user_status ON uploads(user_id, status);

-- Auth lookup on every authenticated request
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_token_hash ON api_tokens(token_hash);

-- Tag retrieval per upload
CREATE INDEX IF NOT EXISTS idx_file_tags_upload_id ON file_tags(upload_id);

-- +goose Down
DROP INDEX IF EXISTS idx_uploads_user_status;
DROP INDEX IF EXISTS idx_api_tokens_token_hash;
DROP INDEX IF EXISTS idx_file_tags_upload_id;
