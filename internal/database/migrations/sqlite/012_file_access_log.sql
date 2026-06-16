-- +goose Up

-- V2-514: file-access *reads* (file_downloaded, file_download_denied) move OFF
-- the tamper-evident audit_log hash-chain into this plain, append-only table.
-- WriteAudit serializes every audit_log write through a process-wide mutex and
-- links each row to the chain head, which (a) forks the chain across instances
-- and (b) bottlenecks throughput — fine for low-volume security events, wrong
-- for high-volume read telemetry served by a reader fleet (parent V2-513).
--
-- This table has NO prev_hash/row_hash: WriteFileAccess is a bare INSERT with no
-- mutex and no head-read, so any number of reader replicas can write it
-- concurrently. Columns otherwise mirror audit_log so the same query/response
-- layer is reused. Security mutations (file upload/delete, auth, config, identity)
-- stay chained in audit_log. The split is forward-only: pre-existing file_* rows
-- remain in audit_log (deleting chained rows would break VerifyAuditChain).
CREATE TABLE file_access_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    user_id INTEGER,
    detail TEXT NOT NULL DEFAULT '',
    ip_address TEXT,
    user_agent TEXT,
    request_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_file_access_log_event_type ON file_access_log(event_type);
CREATE INDEX idx_file_access_log_created_at ON file_access_log(created_at);
CREATE INDEX idx_file_access_log_user_id ON file_access_log(user_id);

-- +goose Down

DROP TABLE file_access_log;
