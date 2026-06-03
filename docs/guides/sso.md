# Single Sign-On (OIDC) setup

*Requires admin permissions.*

Indelible supports OpenID Connect (OIDC) for single sign-on with identity providers like Okta, Microsoft Entra ID (Azure AD), Google, and Keycloak. SSO lets your users log in with their existing company identity instead of an Indelible password.

## Add a provider

1. Go to **Admin → SSO**.
2. Click **+ Add provider** and fill in:
   - **Name** — internal identifier (e.g. `okta`).
   - **Display Name** — the label shown on the login page (e.g. `Sign in with Okta`).
   - **Issuer URL** — your provider's OIDC issuer (e.g. `https://accounts.google.com`, or your Okta org URL).
   - **Client ID** and **Client Secret** — from the application you create in your identity provider.
   - **Scopes** — defaults to `openid email profile`. Space-separated.
3. Save. The provider's **Sign in with …** button appears on the login page.

Client secrets are encrypted at rest with AES-256-GCM.

## How login works

Once a provider is configured, the login page shows its button. Clicking it redirects the user to the identity provider, and on success they're returned to Indelible with an HttpOnly session cookie. Users provisioned by SSO have no Indelible password — they always sign in through the provider.

Each provider has a **`require_email_verified`** flag (strict by default): when on, Indelible only accepts identities whose email the provider marks as verified.

## Per-IdP walkthroughs

For full end-to-end setups — including the identity-provider side, provisioning-behaviour toggles, and troubleshooting — see the walkthroughs in the main guide:

- [Provisioning with Okta](../../USER-GUIDE.md#provisioning-with-okta) — Part A covers SSO.
- [Provisioning with Azure AD](../../USER-GUIDE.md#provisioning-with-azure-ad) — the SSO (OIDC) section.

## Related

- [SCIM provisioning setup](scim.md) — auto-provision users/groups from the same identity provider. SSO (login) and SCIM (provisioning) are typically configured together.
