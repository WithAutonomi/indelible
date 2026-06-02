# SCIM provisioning setup

*Requires admin permissions.*

SCIM 2.0 enables automatic user and group provisioning from identity providers such as Okta, Microsoft Entra ID (Azure AD), and Google Workspace. Users and groups created, updated, or deactivated in your IdP are pushed to Indelible automatically.

## Enable SCIM

1. Go to **Admin → SCIM**.
2. Toggle **SCIM Provisioning** to enabled.
3. Note the **SCIM Base URL** displayed (e.g. `https://your-domain.com/scim/v2`).

## Generate a SCIM token

1. Click **Generate Token**.
2. Enter a descriptive name (e.g. `Okta Production`).
3. Click **Generate**.
4. **Copy the token immediately** — it is shown only once.
5. Use it as the Bearer token in your identity provider's SCIM configuration.

### Token management

- View all tokens with their creation date, last-used timestamp, and status.
- **Revoke** a token when rotating credentials or decommissioning an IdP connection.
- Revoked tokens cannot be reactivated — generate a new one instead.

### Token API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v2/admin/scim/tokens` | Create a SCIM token |
| `GET` | `/api/v2/admin/scim/tokens` | List all SCIM tokens |
| `DELETE` | `/api/v2/admin/scim/tokens/{id}` | Revoke a SCIM token |

## Configure your identity provider

**Okta:**
1. Add a new SCIM 2.0 application.
2. Set **SCIM connector base URL** to `https://your-domain.com/scim/v2`.
3. Set **Unique identifier field** to `userName`.
4. Set **Authentication Mode** to `HTTP Header`.
5. Paste the SCIM token in the **Authorization** field (with the `Bearer ` prefix).
6. Enable **Push New Users**, **Push Profile Updates**, and **Push Groups**.

**Azure AD / Entra ID:**
1. In your Enterprise Application, go to **Provisioning**.
2. Set **Provisioning Mode** to `Automatic`.
3. Set **Tenant URL** to `https://your-domain.com/scim/v2`.
4. Set **Secret Token** to the SCIM token.
5. Click **Test Connection**, then **Save**.
6. Map attributes: `userPrincipalName` → `userName`, `givenName` → `name.givenName`, `surname` → `name.familyName`.

**Google Workspace:**
1. Use the SCIM API endpoint `https://your-domain.com/scim/v2`.
2. Configure with a Bearer token in the Authorization header.

## How SCIM maps to Indelible

| SCIM attribute | Indelible field |
|----------------|-----------------|
| `userName` | `email` |
| `name.givenName` | `first_name` |
| `name.familyName` | `last_name` |
| `active` | `is_active` |
| `externalId` | `external_id` |
| Group `displayName` | `name` |
| Group `members[].value` | group membership |

- SCIM-provisioned users have no password and should authenticate via OIDC/SSO.
- SCIM-provisioned groups default to the `read` permission level.
- SCIM DELETE performs a soft-delete to preserve audit history.

## Per-IdP walkthroughs

For full end-to-end setups — App Catalog setup, the Header-Auth token format, and assignment + group-push flows — see the walkthroughs in the main guide:

- [Provisioning with Okta](../../USER-GUIDE.md#provisioning-with-okta) — Part B covers SCIM.
- [Provisioning with Azure AD](../../USER-GUIDE.md#provisioning-with-azure-ad) — the SCIM section.

## Related

- [Single Sign-On (OIDC) setup](sso.md) — SSO (login) and SCIM (provisioning) are typically configured together.
