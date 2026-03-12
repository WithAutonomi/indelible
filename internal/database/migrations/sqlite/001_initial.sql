-- +goose Up

-- Users (including service accounts)
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    first_name TEXT NOT NULL DEFAULT '',
    last_name TEXT NOT NULL DEFAULT '',
    is_active INTEGER NOT NULL DEFAULT 1,
    is_service_account INTEGER NOT NULL DEFAULT 0,
    email_verified INTEGER NOT NULL DEFAULT 0,
    last_login_at DATETIME,
    max_file_size_bytes INTEGER,
    allowed_file_types TEXT, -- JSON array
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    deleted_at DATETIME
);

-- Groups
CREATE TABLE groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    permission_level TEXT NOT NULL CHECK (permission_level IN ('read', 'write', 'admin')),
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Group membership
CREATE TABLE group_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups(id),
    user_id INTEGER NOT NULL REFERENCES users(id),
    added_by INTEGER REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(group_id, user_id)
);

-- Direct permissions
CREATE TABLE user_permissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    permission_level TEXT NOT NULL CHECK (permission_level IN ('read', 'write', 'admin')),
    granted_by INTEGER REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id)
);

-- API tokens
CREATE TABLE api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    token_hash TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES users(id),
    permissions TEXT NOT NULL DEFAULT '["read"]', -- JSON array
    department TEXT,
    max_file_size_bytes INTEGER,
    allowed_file_types TEXT, -- JSON array
    expires_at DATETIME,
    revoked_at DATETIME,
    revoked_by INTEGER REFERENCES users(id),
    revoke_reason TEXT,
    usage_count INTEGER NOT NULL DEFAULT 0,
    last_used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_api_tokens_user_id ON api_tokens(user_id);
CREATE INDEX idx_api_tokens_uuid ON api_tokens(uuid);

-- Token usage log
CREATE TABLE token_usage_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_id INTEGER NOT NULL REFERENCES api_tokens(id),
    endpoint TEXT NOT NULL,
    method TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_token_usage_log_token_id ON token_usage_log(token_id);
CREATE INDEX idx_token_usage_log_created_at ON token_usage_log(created_at);

-- OIDC providers
CREATE TABLE oidc_providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    issuer_url TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'openid,email,profile',
    is_enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- OIDC user identities (linked accounts)
CREATE TABLE oidc_identities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    provider_id INTEGER NOT NULL REFERENCES oidc_providers(id),
    subject TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(provider_id, subject)
);

-- Wallets
CREATE TABLE wallets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    encrypted_key TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    payment_balance TEXT NOT NULL DEFAULT '0',
    gas_balance TEXT NOT NULL DEFAULT '0',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Uploads
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
    datamap_address TEXT,
    estimated_cost TEXT,
    actual_cost TEXT,
    error_message TEXT,
    temp_path TEXT,
    queued_at DATETIME NOT NULL DEFAULT (datetime('now')),
    processing_at DATETIME,
    completed_at DATETIME,
    failed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_uploads_user_id ON uploads(user_id);
CREATE INDEX idx_uploads_status ON uploads(status);
CREATE INDEX idx_uploads_uuid ON uploads(uuid);

-- File tags
CREATE TABLE file_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    upload_id INTEGER NOT NULL REFERENCES uploads(id),
    tag_key TEXT NOT NULL,
    tag_value TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(upload_id, tag_key)
);

CREATE INDEX idx_file_tags_key_value ON file_tags(tag_key, tag_value);

-- Collections (virtual folders)
CREATE TABLE collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent_id INTEGER REFERENCES collections(id),
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE collection_files (
    collection_id INTEGER NOT NULL REFERENCES collections(id),
    upload_id INTEGER NOT NULL REFERENCES uploads(id),
    added_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, upload_id)
);

-- Wallet transactions
CREATE TABLE transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id INTEGER NOT NULL REFERENCES wallets(id),
    upload_id INTEGER REFERENCES uploads(id),
    tx_type TEXT NOT NULL,
    amount TEXT NOT NULL,
    balance_after TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);

-- System settings (runtime config)
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_by INTEGER REFERENCES users(id)
);

-- Config audit trail
CREATE TABLE config_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    setting_key TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT NOT NULL,
    changed_by INTEGER REFERENCES users(id),
    ip_address TEXT,
    user_agent TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Audit log (permanent)
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    user_id INTEGER,
    detail TEXT NOT NULL DEFAULT '',
    ip_address TEXT,
    user_agent TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_audit_log_event_type ON audit_log(event_type);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);

-- System log (retention-managed)
CREATE TABLE system_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    level TEXT NOT NULL,
    component TEXT NOT NULL,
    message TEXT NOT NULL,
    detail TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_system_log_created_at ON system_log(created_at);

-- Webhook configuration
CREATE TABLE webhook_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    integration_type TEXT NOT NULL DEFAULT 'generic' CHECK (integration_type IN ('generic', 'slack')),
    is_enabled INTEGER NOT NULL DEFAULT 1,
    events TEXT NOT NULL DEFAULT '["completed","failed"]', -- JSON array
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- User notification preferences
CREATE TABLE user_notification_prefs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) UNIQUE,
    webhook_url TEXT,
    events TEXT NOT NULL DEFAULT '[]', -- JSON array
    digest_mode TEXT DEFAULT 'realtime' CHECK (digest_mode IN ('realtime', 'daily', 'weekly')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Storage quotas
CREATE TABLE quotas (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('user', 'group', 'department', 'system')),
    entity_id TEXT, -- user_id, group_id, department name, or NULL for system
    max_bytes INTEGER NOT NULL,
    is_enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(entity_type, entity_id)
);

-- Seed default settings
INSERT INTO settings (key, value) VALUES
    ('maintenance_mode', 'false'),
    ('maintenance_message', ''),
    ('max_upload_size_bytes', '10737418240'),
    ('jwt_expiry_hours', '24'),
    ('default_token_expiry_days', '90'),
    ('max_concurrent_uploads', '1'),
    ('environment_name', 'production'),
    ('cors_allowed_origins', 'http://localhost:5173'),
    ('timezone', 'UTC'),
    ('rate_limit_login_attempts', '5'),
    ('rate_limit_login_window_secs', '60'),
    ('log_retention_enabled', 'false'),
    ('log_retention_days', '30'),
    ('default_user_permissions', '["read"]'),
    ('oidc_enabled', 'false'),
    ('oidc_auto_provision', 'false'),
    ('disk_warning_threshold_pct', '80'),
    ('disk_critical_threshold_pct', '95'),
    ('disk_check_interval_secs', '300');

-- +goose Down
DROP TABLE IF EXISTS user_notification_prefs;
DROP TABLE IF EXISTS quotas;
DROP TABLE IF EXISTS webhook_config;
DROP TABLE IF EXISTS system_log;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS config_audit;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS collection_files;
DROP TABLE IF EXISTS collections;
DROP TABLE IF EXISTS file_tags;
DROP TABLE IF EXISTS uploads;
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS oidc_identities;
DROP TABLE IF EXISTS oidc_providers;
DROP TABLE IF EXISTS token_usage_log;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS user_permissions;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
