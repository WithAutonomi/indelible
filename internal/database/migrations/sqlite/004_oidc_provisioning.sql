-- +goose Up

-- default_group_id is the group new users land in when this provider
-- auto-provisions them on first login. NULL = no automatic group assignment.
ALTER TABLE oidc_providers ADD COLUMN default_group_id INTEGER REFERENCES groups(id);

-- auto_provision controls whether unknown sub/email pairs create a new local
-- user (1) or are rejected with no_account (0). Off by default so a misconfigured
-- provider can't silently grow the user table.
ALTER TABLE oidc_providers ADD COLUMN auto_provision INTEGER NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE oidc_providers DROP COLUMN default_group_id;
ALTER TABLE oidc_providers DROP COLUMN auto_provision;
