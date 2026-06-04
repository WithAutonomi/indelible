-- +goose Up

-- See sqlite/008_audit_hash_chain.sql for rationale (V2-452 audit hash-chain).
ALTER TABLE audit_log ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN row_hash TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE audit_log DROP COLUMN prev_hash;
ALTER TABLE audit_log DROP COLUMN row_hash;
