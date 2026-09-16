-- +goose Up

-- V2-1086: per-upload payment provenance. payment_backend records WHO settled the
-- upload's payment was settled — 'local' (this instance's wallet signed) or
-- 'hosted' (the payment gateway paid from the tenant's credits). Without it
-- the instance-wide payment_backend config is the only tell, and history goes
-- ambiguous the moment an instance switches modes. gateway_payment_key is
-- the gateway's content-derived batch idempotency key — the stable join to
-- the gateway's payments ledger (and from there to the on-chain tx). Both
-- NULL for unpaid uploads (dedup/already_stored) and pre-existing rows.
ALTER TABLE uploads ADD COLUMN payment_backend TEXT;
ALTER TABLE uploads ADD COLUMN gateway_payment_key TEXT;

-- +goose Down
ALTER TABLE uploads DROP COLUMN gateway_payment_key;
ALTER TABLE uploads DROP COLUMN payment_backend;
