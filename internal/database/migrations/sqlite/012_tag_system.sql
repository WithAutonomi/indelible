-- +goose Up

-- Missing indexes identified in code review
CREATE INDEX IF NOT EXISTS idx_file_tags_upload_id ON file_tags(upload_id);
CREATE INDEX IF NOT EXISTS idx_collection_files_upload_id ON collection_files(upload_id);

-- Auto-tag rules: match conditions that apply tags to uploads automatically
CREATE TABLE IF NOT EXISTS tag_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    match_field TEXT NOT NULL,      -- 'content_type', 'filename', 'file_size', 'visibility'
    match_op TEXT NOT NULL,         -- 'equals', 'regex', 'contains', 'gt', 'lt'
    match_value TEXT NOT NULL,      -- the value to match against
    apply_key TEXT NOT NULL,        -- tag key to set
    apply_value TEXT NOT NULL,      -- tag value to set
    priority INTEGER NOT NULL DEFAULT 100, -- lower = runs first
    is_enabled INTEGER NOT NULL DEFAULT 1,
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_tag_rules_enabled_priority ON tag_rules(is_enabled, priority);

-- Collection-level tags: inherited by files when added to the collection
CREATE TABLE IF NOT EXISTS collection_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    tag_key TEXT NOT NULL,
    tag_value TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(collection_id, tag_key)
);

CREATE INDEX IF NOT EXISTS idx_collection_tags_collection_id ON collection_tags(collection_id);

-- +goose Down
DROP INDEX IF EXISTS idx_collection_tags_collection_id;
DROP TABLE IF EXISTS collection_tags;
DROP INDEX IF EXISTS idx_tag_rules_enabled_priority;
DROP TABLE IF EXISTS tag_rules;
DROP INDEX IF EXISTS idx_collection_files_upload_id;
DROP INDEX IF EXISTS idx_file_tags_upload_id;
