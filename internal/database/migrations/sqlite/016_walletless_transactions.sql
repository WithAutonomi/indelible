-- +goose Up

-- V2-929 wallet-less hosted mode: hosted payments are settled by the
-- payment gateway's treasury, not by any wallet record — their transaction
-- rows carry wallet_id NULL. SQLite cannot drop NOT NULL in place, so the
-- table is rebuilt (same pattern as 014).
ALTER TABLE transactions RENAME TO transactions_old;
CREATE TABLE transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id INTEGER REFERENCES wallets(id),
    upload_id INTEGER REFERENCES uploads(id),
    tx_type TEXT NOT NULL,
    amount TEXT NOT NULL,
    balance_after TEXT NOT NULL,
    tx_hash TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO transactions (id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at)
    SELECT id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at FROM transactions_old;
DROP TABLE transactions_old;
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);

-- +goose Down
ALTER TABLE transactions RENAME TO transactions_old;
CREATE TABLE transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id INTEGER NOT NULL REFERENCES wallets(id),
    upload_id INTEGER REFERENCES uploads(id),
    tx_type TEXT NOT NULL,
    amount TEXT NOT NULL,
    balance_after TEXT NOT NULL,
    tx_hash TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO transactions (id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at)
    SELECT id, COALESCE(wallet_id, 0), upload_id, tx_type, amount, balance_after, tx_hash, created_at FROM transactions_old;
DROP TABLE transactions_old;
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);
