-- +goose Up

-- See sqlite/009_audit_anchors.sql for rationale (V2-453 audit-chain anchoring).
CREATE TABLE audit_anchors (
    id BIGSERIAL PRIMARY KEY,
    head_hash TEXT NOT NULL,
    row_count BIGINT NOT NULL,
    network_address TEXT NOT NULL,
    tx_hash TEXT NOT NULL DEFAULT '',
    anchored_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE audit_anchors;
