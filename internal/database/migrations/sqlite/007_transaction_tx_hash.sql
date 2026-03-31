-- +goose Up

-- Add on-chain transaction hash for auditability
ALTER TABLE transactions ADD COLUMN tx_hash TEXT;

-- +goose Down
ALTER TABLE transactions DROP COLUMN tx_hash;
