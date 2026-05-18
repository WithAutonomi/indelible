-- +goose Up

ALTER TABLE oidc_providers ADD COLUMN default_group_id BIGINT REFERENCES groups(id);
ALTER TABLE oidc_providers ADD COLUMN auto_provision BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

ALTER TABLE oidc_providers DROP COLUMN default_group_id;
ALTER TABLE oidc_providers DROP COLUMN auto_provision;
