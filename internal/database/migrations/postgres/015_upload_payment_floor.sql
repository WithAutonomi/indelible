-- +goose Up

-- V2-1773: which payments count against an upload's current authorization to
-- pay. A payment recorded for the upload with transactions.id above this floor
-- was made since the upload was created or last retried by an operator, so an
-- automatic re-run must not pay again. Creation leaves it at 0 (every payment
-- counts); an operator Retry raises it to the upload's latest transaction id,
-- which authorizes one new payment. Transaction ids only grow (BIGSERIAL), so
-- unlike comparing created_at with queued_at, this holds when a Retry lands
-- in the same second as the payment before it.
ALTER TABLE uploads ADD COLUMN payment_floor_txn_id BIGINT NOT NULL DEFAULT 0;

-- Backfill so every existing upload gets the same answer the
-- created_at/queued_at comparison it replaces gave (Retry has always set
-- queued_at), whatever the order of its transaction ids and timestamps:
-- - a payment at or after queued_at (including the same second) counted as
--   paid, so the floor goes just below the lowest such transaction id;
-- - otherwise every payment predates the last Retry and none counted, so the
--   floor goes to the highest transaction id;
-- - an upload with no transactions starts at 0.
UPDATE uploads SET payment_floor_txn_id = COALESCE(
    (SELECT MIN(t.id) - 1 FROM transactions t
     WHERE t.upload_id = uploads.id AND t.created_at >= uploads.queued_at),
    (SELECT MAX(t.id) FROM transactions t WHERE t.upload_id = uploads.id),
    0);

-- +goose Down

ALTER TABLE uploads DROP COLUMN payment_floor_txn_id;
