-- +goose Up

ALTER TABLE oidc_providers ADD COLUMN require_email_verified BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down

ALTER TABLE oidc_providers DROP COLUMN require_email_verified;
