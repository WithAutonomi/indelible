-- +goose Up

-- V2-453: records each time the audit-log hash-chain head is anchored to
-- Autonomi. network_address is the content address of the anchored digest blob
-- (head_hash + row_count), independently retrievable to prove the chain head
-- existed at anchored_at — defending against a full local-chain rewrite.
CREATE TABLE audit_anchors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    head_hash TEXT NOT NULL,
    row_count INTEGER NOT NULL,
    network_address TEXT NOT NULL,
    tx_hash TEXT NOT NULL DEFAULT '',
    anchored_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down

DROP TABLE audit_anchors;
