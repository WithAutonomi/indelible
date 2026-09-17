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

-- Rolling back to a NOT NULL wallet_id cannot represent hosted-payment rows
-- (wallet_id NULL: the gateway's treasury paid, no wallet exists). They are
-- DROPPED here, deliberately: the previous COALESCE(wallet_id, 0) rewrite
-- pointed them at a wallet 0 that does not exist, which fails under the
-- foreign_keys pragma this app opens SQLite with (#163 review, V2-1269).
-- The gateway's ledger remains the record of those payments (payment_key on
-- uploads, dropped by 015's own Down); export them first if you need them.
ALTER TABLE transactions RENAME TO transactions_old;
DELETE FROM transactions_old WHERE wallet_id IS NULL;
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
    SELECT id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at FROM transactions_old;
DROP TABLE transactions_old;
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);
