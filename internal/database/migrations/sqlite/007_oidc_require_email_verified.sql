-- +goose Up

-- require_email_verified gates the OIDC auto-provision check on the id_token's
-- email_verified claim. Strict by default (1) so operators don't accidentally
-- accept unverified emails. Operators turn this off (0) per provider when their
-- IdP doesn't emit the claim — Okta integrator tenants, Azure AD, generic OIDC
-- providers that follow §5.1 of OIDC Core where the claim is optional. The
-- email-collision guard at oidc_login.resolveOrProvisionUser still applies, so
-- existing-user takeover via a re-registered IdP account is still blocked.
ALTER TABLE oidc_providers ADD COLUMN require_email_verified INTEGER NOT NULL DEFAULT 1;

-- +goose Down

ALTER TABLE oidc_providers DROP COLUMN require_email_verified;
