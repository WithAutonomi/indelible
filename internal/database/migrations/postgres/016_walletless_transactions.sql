-- +goose Up

-- V2-929 wallet-less hosted mode: hosted payments are settled by the
-- payment gateway's treasury, not by any wallet record — their transaction
-- rows carry wallet_id NULL.
ALTER TABLE transactions ALTER COLUMN wallet_id DROP NOT NULL;

-- +goose Down
ALTER TABLE transactions ALTER COLUMN wallet_id SET NOT NULL;
