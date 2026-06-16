-- +goose Up

-- See sqlite/012_file_access_log.sql for rationale (V2-514: move high-volume
-- file-read telemetry off the audit_log hash-chain into a plain append-only
-- table so a reader fleet can write it concurrently without forking the chain
-- or serializing on a process-wide mutex).
CREATE TABLE file_access_log (
    id BIGSERIAL PRIMARY KEY,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    user_id BIGINT,
    detail TEXT NOT NULL DEFAULT '',
    ip_address TEXT,
    user_agent TEXT,
    request_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_file_access_log_event_type ON file_access_log(event_type);
CREATE INDEX idx_file_access_log_created_at ON file_access_log(created_at);
CREATE INDEX idx_file_access_log_user_id ON file_access_log(user_id);

-- +goose Down

DROP TABLE file_access_log;
