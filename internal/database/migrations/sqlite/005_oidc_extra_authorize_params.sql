-- +goose Up

-- extra_authorize_params is a JSON-encoded {key: value} map of IdP-specific
-- query parameters to append to the OAuth2 authorize URL.  Empty string = no
-- extra params (the common case).  Primary motivating use is Google Workspace
-- domain restriction via hd=company.com (V2-313); also fits MS prompt= /
-- AAD domain_hint and any future single-string parameter quirks.
ALTER TABLE oidc_providers ADD COLUMN extra_authorize_params TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE oidc_providers DROP COLUMN extra_authorize_params;
