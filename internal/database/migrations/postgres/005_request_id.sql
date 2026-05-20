-- +goose Up

-- See sqlite/005_request_id.sql for rationale.
ALTER TABLE audit_log ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE system_log ADD COLUMN request_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_audit_log_request_id ON audit_log(request_id) WHERE request_id != '';
CREATE INDEX idx_system_log_request_id ON system_log(request_id) WHERE request_id != '';

-- +goose Down

DROP INDEX IF EXISTS idx_audit_log_request_id;
DROP INDEX IF EXISTS idx_system_log_request_id;
ALTER TABLE audit_log DROP COLUMN request_id;
ALTER TABLE system_log DROP COLUMN request_id;
