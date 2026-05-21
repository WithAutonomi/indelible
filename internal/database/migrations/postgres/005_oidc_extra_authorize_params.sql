-- +goose Up

ALTER TABLE oidc_providers ADD COLUMN extra_authorize_params TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE oidc_providers DROP COLUMN extra_authorize_params;
