-- +goose Up

-- V2-875: the 'already_stored' status (V2-399 content-addressed dedup — a
-- re-upload whose chunks were all already on the network) has been written by
-- the worker since V2-399 shipped, but the status CHECK constraint never
-- listed it, so the transition fails on any database built from these
-- migrations. Found by the #154 panel review. Postgres names the inline
-- column CHECK uploads_status_check.
ALTER TABLE uploads DROP CONSTRAINT uploads_status_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_status_check
    CHECK (status IN ('queued', 'processing', 'completed', 'failed', 'already_stored'));

-- +goose Down

-- Rows already in 'already_stored' would violate the original constraint, so
-- they are folded into 'completed' (both are terminal stored states; the
-- distinction is a UI nicety).
UPDATE uploads SET status = 'completed' WHERE status = 'already_stored';
ALTER TABLE uploads DROP CONSTRAINT uploads_status_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_status_check
    CHECK (status IN ('queued', 'processing', 'completed', 'failed'));
