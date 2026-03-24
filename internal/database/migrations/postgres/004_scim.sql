-- +goose Up

-- Add external_id to users for SCIM provisioning
ALTER TABLE users ADD COLUMN external_id TEXT;
CREATE UNIQUE INDEX idx_users_external_id ON users(external_id) WHERE external_id IS NOT NULL;

-- Add external_id to groups for SCIM provisioning
ALTER TABLE groups ADD COLUMN external_id TEXT;
CREATE UNIQUE INDEX idx_groups_external_id ON groups(external_id) WHERE external_id IS NOT NULL;

-- SCIM bearer tokens (separate from API tokens — no user owner, no expiry, no permissions)
CREATE TABLE scim_tokens (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT NOT NULL REFERENCES users(id),
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

-- Seed SCIM setting
INSERT INTO settings (key, value) VALUES ('scim_enabled', 'false');

-- +goose Down
DELETE FROM settings WHERE key = 'scim_enabled';
DROP TABLE IF EXISTS scim_tokens;
DROP INDEX IF EXISTS idx_groups_external_id;
DROP INDEX IF EXISTS idx_users_external_id;
