-- +goose Up

-- V2-433 (A3.2): add ON DELETE CASCADE to collection_files' foreign keys.
-- App code already removes file associations when a collection or upload is
-- deleted, but the DB-level constraint was plain REFERENCES (no cascade), so a
-- delete that bypasses the app path (manual SQL, future code) would orphan rows.
-- This matches the sibling collection_tags table, which already cascades.
ALTER TABLE collection_files DROP CONSTRAINT IF EXISTS collection_files_collection_id_fkey;
ALTER TABLE collection_files ADD CONSTRAINT collection_files_collection_id_fkey
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE;

ALTER TABLE collection_files DROP CONSTRAINT IF EXISTS collection_files_upload_id_fkey;
ALTER TABLE collection_files ADD CONSTRAINT collection_files_upload_id_fkey
    FOREIGN KEY (upload_id) REFERENCES uploads(id) ON DELETE CASCADE;

-- +goose Down

ALTER TABLE collection_files DROP CONSTRAINT IF EXISTS collection_files_collection_id_fkey;
ALTER TABLE collection_files ADD CONSTRAINT collection_files_collection_id_fkey
    FOREIGN KEY (collection_id) REFERENCES collections(id);

ALTER TABLE collection_files DROP CONSTRAINT IF EXISTS collection_files_upload_id_fkey;
ALTER TABLE collection_files ADD CONSTRAINT collection_files_upload_id_fkey
    FOREIGN KEY (upload_id) REFERENCES uploads(id);
