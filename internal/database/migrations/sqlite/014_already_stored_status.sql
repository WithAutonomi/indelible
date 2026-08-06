-- +goose Up

-- V2-875: the 'already_stored' status (V2-399 content-addressed dedup — a
-- re-upload whose chunks were all already on the network) has been written by
-- the worker since V2-399 shipped, but the status CHECK constraint never
-- listed it, so the transition fails on any database built from these
-- migrations. Found by the #154 panel review.
--
-- SQLite cannot alter a CHECK in place, so uploads is rebuilt. Renaming a
-- table rewrites the FK clauses of every table that references it (and the
-- legacy_alter_table escape hatch is ignored inside goose's transaction), so
-- the three referencing tables — file_tags, collection_files, transactions —
-- are rebuilt afterwards against the new uploads, the 011 pattern. All child
-- rows are copied before the old tables are dropped; dropping a child table
-- deletes only child rows, which violates no FK, and uploads_old is dropped
-- last, once nothing references it.

ALTER TABLE uploads RENAME TO uploads_old;

CREATE TABLE uploads (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    token_id INTEGER REFERENCES api_tokens(id),
    filename TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('public', 'private')),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'completed', 'failed', 'already_stored')),
    status_detail TEXT,
    datamap_address TEXT,
    data_map TEXT,
    estimated_cost TEXT,
    actual_cost TEXT,
    error_message TEXT,
    temp_path TEXT,
    backoff_until DATETIME,
    backoff_attempt INTEGER NOT NULL DEFAULT 0,
    last_quoted_cost TEXT,
    queued_at DATETIME NOT NULL DEFAULT (datetime('now')),
    processing_at DATETIME,
    completed_at DATETIME,
    failed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    cache_key TEXT
);

INSERT INTO uploads (id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
    status_detail, datamap_address, data_map, estimated_cost, actual_cost, error_message, temp_path,
    backoff_until, backoff_attempt, last_quoted_cost, queued_at, processing_at, completed_at, failed_at, created_at, cache_key)
SELECT id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
    status_detail, datamap_address, data_map, estimated_cost, actual_cost, error_message, temp_path,
    backoff_until, backoff_attempt, last_quoted_cost, queued_at, processing_at, completed_at, failed_at, created_at, cache_key
FROM uploads_old;

CREATE TABLE file_tags_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    upload_id INTEGER NOT NULL REFERENCES uploads(id),
    tag_key TEXT NOT NULL,
    tag_value TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO file_tags_new (id, upload_id, tag_key, tag_value, created_at)
    SELECT id, upload_id, tag_key, tag_value, created_at FROM file_tags;
DROP TABLE file_tags;
ALTER TABLE file_tags_new RENAME TO file_tags;
CREATE INDEX idx_file_tags_upload_key ON file_tags(upload_id, tag_key);
CREATE INDEX idx_file_tags_key_value ON file_tags(tag_key, tag_value);
CREATE INDEX idx_file_tags_upload_id ON file_tags(upload_id);

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

CREATE TABLE transactions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id INTEGER NOT NULL REFERENCES wallets(id),
    upload_id INTEGER REFERENCES uploads(id),
    tx_type TEXT NOT NULL,
    amount TEXT NOT NULL,
    balance_after TEXT NOT NULL,
    tx_hash TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO transactions_new (id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at)
    SELECT id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at FROM transactions;
DROP TABLE transactions;
ALTER TABLE transactions_new RENAME TO transactions;
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);

DROP TABLE uploads_old;

-- Index names are global and follow the renamed table until it is dropped,
-- so the new uploads indexes can only be created now.
CREATE INDEX idx_uploads_user_id ON uploads(user_id);
CREATE INDEX idx_uploads_status ON uploads(status);
CREATE INDEX idx_uploads_uuid ON uploads(uuid);
CREATE INDEX idx_uploads_user_status ON uploads(user_id, status);
CREATE INDEX idx_uploads_backoff ON uploads(status, backoff_until);
CREATE INDEX idx_uploads_status_processing ON uploads(status, processing_at);
CREATE INDEX idx_uploads_cache_key ON uploads(cache_key);


-- +goose Down

-- Reverse rebuild with the original CHECK. Rows already in 'already_stored'
-- are folded into 'completed' (both are terminal stored states; the
-- distinction is a UI nicety). Same child-rebuild dance in reverse.

ALTER TABLE uploads RENAME TO uploads_old;

CREATE TABLE uploads (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    token_id INTEGER REFERENCES api_tokens(id),
    filename TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('public', 'private')),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
    status_detail TEXT,
    datamap_address TEXT,
    data_map TEXT,
    estimated_cost TEXT,
    actual_cost TEXT,
    error_message TEXT,
    temp_path TEXT,
    backoff_until DATETIME,
    backoff_attempt INTEGER NOT NULL DEFAULT 0,
    last_quoted_cost TEXT,
    queued_at DATETIME NOT NULL DEFAULT (datetime('now')),
    processing_at DATETIME,
    completed_at DATETIME,
    failed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    cache_key TEXT
);

INSERT INTO uploads (id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
    status_detail, datamap_address, data_map, estimated_cost, actual_cost, error_message, temp_path,
    backoff_until, backoff_attempt, last_quoted_cost, queued_at, processing_at, completed_at, failed_at, created_at, cache_key)
SELECT id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility,
    CASE WHEN status = 'already_stored' THEN 'completed' ELSE status END,
    status_detail, datamap_address, data_map, estimated_cost, actual_cost, error_message, temp_path,
    backoff_until, backoff_attempt, last_quoted_cost, queued_at, processing_at, completed_at, failed_at, created_at, cache_key
FROM uploads_old;

CREATE TABLE file_tags_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    upload_id INTEGER NOT NULL REFERENCES uploads(id),
    tag_key TEXT NOT NULL,
    tag_value TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO file_tags_new (id, upload_id, tag_key, tag_value, created_at)
    SELECT id, upload_id, tag_key, tag_value, created_at FROM file_tags;
DROP TABLE file_tags;
ALTER TABLE file_tags_new RENAME TO file_tags;
CREATE INDEX idx_file_tags_upload_key ON file_tags(upload_id, tag_key);
CREATE INDEX idx_file_tags_key_value ON file_tags(tag_key, tag_value);
CREATE INDEX idx_file_tags_upload_id ON file_tags(upload_id);

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

CREATE TABLE transactions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id INTEGER NOT NULL REFERENCES wallets(id),
    upload_id INTEGER REFERENCES uploads(id),
    tx_type TEXT NOT NULL,
    amount TEXT NOT NULL,
    balance_after TEXT NOT NULL,
    tx_hash TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO transactions_new (id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at)
    SELECT id, wallet_id, upload_id, tx_type, amount, balance_after, tx_hash, created_at FROM transactions;
DROP TABLE transactions;
ALTER TABLE transactions_new RENAME TO transactions;
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);

DROP TABLE uploads_old;

-- Index names are global and follow the renamed table until it is dropped,
-- so the new uploads indexes can only be created now.
CREATE INDEX idx_uploads_user_id ON uploads(user_id);
CREATE INDEX idx_uploads_status ON uploads(status);
CREATE INDEX idx_uploads_uuid ON uploads(uuid);
CREATE INDEX idx_uploads_user_status ON uploads(user_id, status);
CREATE INDEX idx_uploads_backoff ON uploads(status, backoff_until);
CREATE INDEX idx_uploads_status_processing ON uploads(status, processing_at);
CREATE INDEX idx_uploads_cache_key ON uploads(cache_key);

