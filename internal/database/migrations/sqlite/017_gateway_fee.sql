-- +goose Up

-- V2-1098: the payment gateway's per-batch network fee, stamped with the
-- payment so the upload's cost display can itemize it ("cost includes the
-- fee that covered the chain's gas"). actual_cost holds the GROSS debit
-- (batch total + this fee) for hosted uploads — the number the tenant's
-- credits actually dropped by. NULL for local-mode and pre-existing rows,
-- and for hosted uploads paid before the gateway charged a fee.
ALTER TABLE uploads ADD COLUMN gateway_fee_atto TEXT;

-- +goose Down
ALTER TABLE uploads DROP COLUMN gateway_fee_atto;
