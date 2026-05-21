-- +goose Up

-- request_id links a log row back to the chi-generated X-Request-Id header
-- so an operator can correlate audit/system rows produced by the same
-- request, and tie either back to slog stdout entries that also carry it.
-- Empty string when the writer isn't part of an HTTP request (e.g. workers).
ALTER TABLE audit_log ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE system_log ADD COLUMN request_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_audit_log_request_id ON audit_log(request_id) WHERE request_id != '';
CREATE INDEX idx_system_log_request_id ON system_log(request_id) WHERE request_id != '';

-- +goose Down

DROP INDEX IF EXISTS idx_audit_log_request_id;
DROP INDEX IF EXISTS idx_system_log_request_id;
ALTER TABLE audit_log DROP COLUMN request_id;
ALTER TABLE system_log DROP COLUMN request_id;
