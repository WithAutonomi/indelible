-- +goose Up

-- V2-873: fleet-wide download-cache purge propagation.
--
-- uploads.cache_key is the serve-path cache key (KeyForIdentifier over
-- data_map when present, else datamap_address — the same preference order
-- downloadETag uses), set when an upload reaches a terminal stored status and
-- backfilled at writer boot for pre-existing rows. Cache keys are one-way
-- digests, so without this column no instance can map its cached files back
-- to upload rows (needed by boot reconciliation).
ALTER TABLE uploads ADD COLUMN cache_key TEXT;
CREATE INDEX idx_uploads_cache_key ON uploads(cache_key);

-- cache_purge_log is the delete fan-out: DeleteUpload's service half appends
-- the deleted upload's cache keys here (insert-first, so a failed delete can
-- at worst cause a spurious purge that re-warms), and every instance's cache
-- sweep worker consumes the tail each tick, dropping local entries. Rows are
-- pruned after a retention window by writer-role instances only (reader
-- discipline, V2-514); instances offline longer than the retention are
-- covered by boot reconciliation against uploads.cache_key.
CREATE TABLE cache_purge_log (
    id BIGSERIAL PRIMARY KEY,
    cache_key TEXT NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_cache_purge_log_deleted_at ON cache_purge_log(deleted_at);

-- +goose Down

DROP TABLE cache_purge_log;
DROP INDEX idx_uploads_cache_key;
ALTER TABLE uploads DROP COLUMN cache_key;
