-- +goose Up

-- V2-1773: which payments count against an upload's current authorization to
-- pay. A payment recorded for the upload with transactions.id above this floor
-- was made since the upload was created or last retried by an operator, so an
-- automatic re-run must not pay again. Creation leaves it at 0 (every payment
-- counts); an operator Retry raises it to the upload's latest transaction id,
-- which authorizes one new payment. Transaction ids only grow
-- (AUTOINCREMENT), so unlike comparing created_at with queued_at, this holds
-- when a Retry lands in the same second as the payment before it.
ALTER TABLE uploads ADD COLUMN payment_floor_txn_id INTEGER NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE uploads DROP COLUMN payment_floor_txn_id;
