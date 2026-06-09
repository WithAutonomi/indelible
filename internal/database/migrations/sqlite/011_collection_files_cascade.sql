-- +goose Up

-- V2-433 (A3.2): add ON DELETE CASCADE to collection_files' foreign keys.
-- See postgres/011 for rationale. SQLite can't alter a constraint in place, so
-- rebuild the table (it has no incoming references, so this is safe). Runs inside
-- goose's transaction with foreign_keys=ON; the copy satisfies the existing FKs.
CREATE TABLE collection_files_new (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    upload_id INTEGER NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    added_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, upload_id)
);
INSERT INTO collection_files_new (collection_id, upload_id, added_at)
    SELECT collection_id, upload_id, added_at FROM collection_files;
DROP TABLE collection_files;
ALTER TABLE collection_files_new RENAME TO collection_files;
CREATE INDEX idx_collection_files_upload_id ON collection_files(upload_id);

-- +goose Down

CREATE TABLE collection_files_old (
    collection_id INTEGER NOT NULL REFERENCES collections(id),
    upload_id INTEGER NOT NULL REFERENCES uploads(id),
    added_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, upload_id)
);
INSERT INTO collection_files_old (collection_id, upload_id, added_at)
    SELECT collection_id, upload_id, added_at FROM collection_files;
DROP TABLE collection_files;
ALTER TABLE collection_files_old RENAME TO collection_files;
CREATE INDEX idx_collection_files_upload_id ON collection_files(upload_id);
