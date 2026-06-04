-- +goose Up

-- V2-452: hash-chain the audit log so post-hoc tampering is detectable.
-- Each chained row stores prev_hash (the previous chained row's row_hash) and
-- row_hash = SHA256(prev_hash + NUL-joined content fields). Rows written before
-- this migration have empty hashes and are excluded from chain verification
-- (they predate the chain; they cannot be retroactively hashed).
ALTER TABLE audit_log ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN row_hash TEXT NOT NULL DEFAULT '';

-- +goose Down

DROP INDEX IF EXISTS idx_audit_log_row_hash;
ALTER TABLE audit_log DROP COLUMN prev_hash;
ALTER TABLE audit_log DROP COLUMN row_hash;
