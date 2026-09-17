-- +goose Up

-- V2-929 wallet-less hosted mode: hosted payments are settled by the
-- payment gateway's treasury, not by any wallet record — their transaction
-- rows carry wallet_id NULL.
ALTER TABLE transactions ALTER COLUMN wallet_id DROP NOT NULL;

-- +goose Down

-- SET NOT NULL fails while any hosted-payment row (wallet_id NULL — the
-- gateway's treasury paid, no wallet exists) is present, and the pre-016
-- schema cannot hold them. They are DROPPED here, deliberately, so the
-- rollback completes; the gateway's ledger remains the record of those
-- payments. Export them first if you need them (#163 review, V2-1269).
DELETE FROM transactions WHERE wallet_id IS NULL;
ALTER TABLE transactions ALTER COLUMN wallet_id SET NOT NULL;
