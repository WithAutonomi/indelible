# Indelible User Guide

Indelible is an enterprise gateway for the Autonomi decentralized storage network. It provides file upload/download, wallet management, access control, and analytics — all served from a single binary.

---

## Table of Contents

1. [Getting Started](#getting-started)
2. [Configuration](#configuration)
3. [Authentication](#authentication)
4. [File Uploads](#file-uploads)
5. [Collections & Tags](#collections--tags)
6. [API Tokens](#api-tokens)
7. [Admin: User Management](#admin-user-management)
8. [Admin: Wallets](#admin-wallets)
9. [Admin: Storage Quotas](#admin-storage-quotas)
10. [Admin: System Settings](#admin-system-settings)
11. [Admin: Webhooks](#admin-webhooks)
12. [Admin: OIDC / SSO Providers](#admin-oidc--sso-providers)
13. [Admin: SCIM Provisioning](#admin-scim-provisioning)
14. [Provisioning with Okta](#provisioning-with-okta)
15. [Provisioning with Azure AD](#provisioning-with-azure-ad)
16. [Admin: Analytics](#admin-analytics)
17. [Admin: Logs](#admin-logs)
18. [Maintenance Mode](#maintenance-mode)
19. [Rate Limiting](#rate-limiting)
20. [Disk Space Monitoring](#disk-space-monitoring)
21. [API Consumer Guide](#api-consumer-guide)
22. [Deployment](#deployment)
23. [API Reference](#api-reference)

---

## Getting Started

### Quick Start

```bash
# Set required JWT secret
export INDELIBLE_JWT_SECRET="your-secret-key-at-least-32-chars"

# Seed the first administrator. Self-registration is disabled by default, so
# this is how the initial admin is created on a fresh instance.
export INDELIBLE_ADMIN_EMAIL="you@example.com"
export INDELIBLE_ADMIN_PASSWORD="a-strong-password"

# Run with defaults (SQLite, port 8080)
./indelible --config indelible.toml
```

Open `http://localhost:8080` and **log in with the admin email/password you set above**. The bootstrap admin is created on first boot only (it's ignored once an admin exists). For Docker/Kubernetes secrets, set `INDELIBLE_ADMIN_PASSWORD_FILE` to a mounted file instead of `INDELIBLE_ADMIN_PASSWORD` — it takes precedence. To allow others to sign up, an admin enables it in **Admin → Settings** (`registration_enabled`); self-registered users get read-only access.

### Requirements

- **antd daemon** — either managed automatically (`INDELIBLE_ANTD_MANAGED=true`) or running externally (default: `http://localhost:8082`)
- **Database** — SQLite (default, zero-config) or PostgreSQL 14+

---

## Configuration

Indelible reads configuration from a TOML file and/or environment variables. Environment variables take precedence.

### Config File (`indelible.toml`)

```toml
port = 8080
db_url = "sqlite:///var/lib/indelible/data.db"
antd_url = "http://localhost:8082"
data_dir = "/var/lib/indelible"
antd_managed = false        # set true to spawn antd automatically
antd_bin = "antd"           # path to antd binary (searches PATH)
jwt_secret = "your-secret-key-at-least-32-chars"
debug = false
cors_allowed_origins = ["https://files.acme.com"]
trusted_proxies = ["127.0.0.1/32"]
base_url = "https://files.acme.com"
wallet_encryption_key = "64-hex-char-key-for-aes-256-gcm"

[smtp]
host = "smtp.example.com"
port = 587
username = "noreply@example.com"
password = "smtp-password"
from = "noreply@example.com"
use_tls = true
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `INDELIBLE_PORT` | HTTP listen port | `8080` |
| `INDELIBLE_DB_URL` | Database connection string | `sqlite:///var/lib/indelible/data.db` |
| `INDELIBLE_ANTD_URL` | antd daemon URL | `http://localhost:8082` |
| `INDELIBLE_DATA_DIR` | Data directory for temp files | `/var/lib/indelible` |
| `INDELIBLE_JWT_SECRET` | **Required.** Secret for JWT signing | — |
| `INDELIBLE_DEBUG` | Enable debug logging | `false` |
| `INDELIBLE_CORS_ORIGINS` | Comma-separated allowed origins | — |
| `INDELIBLE_TRUSTED_PROXIES` | Comma-separated CIDR ranges | — |
| `INDELIBLE_BASE_URL` | External URL for links in emails | — |
| `INDELIBLE_WALLET_ENCRYPTION_KEY` | 64-char hex key for wallet encryption | placeholder (insecure) |
| `INDELIBLE_SMTP_HOST` | SMTP server hostname | — |
| `INDELIBLE_SMTP_PORT` | SMTP server port | `587` |
| `INDELIBLE_SMTP_USERNAME` | SMTP username | — |
| `INDELIBLE_SMTP_PASSWORD` | SMTP password | — |
| `INDELIBLE_SMTP_FROM` | Sender email address | — |
| `INDELIBLE_SMTP_USE_TLS` | Use STARTTLS | `false` |
| `INDELIBLE_ANTD_MANAGED` | Spawn and manage antd as child process | `false` |
| `INDELIBLE_ANTD_BIN` | Path to antd binary | `antd` (searches PATH) |
| `INDELIBLE_WORKERS_ENABLED` | Run the background worker tier + DB migrations. Set `false` for stateless reader replicas (HTTP/downloads only) in a read/write role split | `true` |

### Managed antd

When `antd_managed = true` (or `INDELIBLE_ANTD_MANAGED=true`), indelible will:
1. Check if antd is already running (via port file auto-discovery)
2. If not, spawn antd on a random free port
3. Wait for antd to write its port file and pass health checks
4. Monitor the process and restart on crash (up to 3 times)
5. Stop antd when indelible shuts down

This is recommended for development and single-node deployments. For production with separate antd instances (e.g. Docker Compose), set `INDELIBLE_ANTD_URL` explicitly instead.

### Database Selection

- **SQLite** (default): `db_url = "sqlite:///path/to/data.db"` — zero config, good for small deployments
- **PostgreSQL**: `db_url = "postgres://user:pass@host/indelible"` — recommended for 10+ concurrent users

### Wallet Encryption Key

Wallet private keys are encrypted at rest using AES-256-GCM. Generate a secure key:

```bash
openssl rand -hex 32
```

Set the resulting 64-character hex string as `INDELIBLE_WALLET_ENCRYPTION_KEY`. **If you lose this key, encrypted wallet keys become unrecoverable.**

---

## Authentication

### Registration

Self-registration is **disabled by default**. An admin enables it in **Admin → Settings** (`registration_enabled`). The first administrator is not created here — it is seeded from `INDELIBLE_ADMIN_EMAIL` / `INDELIBLE_ADMIN_PASSWORD` at startup (see [Getting Started](#getting-started)).

When registration is enabled:

1. Navigate to `/register` in your browser
2. Enter email, password (minimum 8 characters), first name, last name
3. Submit. Registration always returns the same neutral response and does **not** log you in — an anti-enumeration measure: it never reveals whether the address is already registered
4. Sign in at `/login` with your new credentials. Self-registered users receive **read-only** permissions; an admin can grant more
5. A verification email is sent if SMTP is configured (verification is not required to sign in)

### Login

Navigate to `/login` and enter your email and password. Login is rate-limited to **5 attempts per minute per IP** to prevent brute force attacks.

### Password Reset

1. Click "Forgot password?" on the login page
2. Enter your email address — a reset link is sent via the configured notifier (see [Email Delivery](#email-delivery))
3. Click the link in your email and set a new password
4. The response is always the same regardless of whether the email exists (prevents email enumeration)

### Email Verification

A verification email is sent automatically on registration. Click the link to verify your email address.

- Verification tokens expire after **24 hours**
- To resend: call `POST /api/v2/me/resend-verification` (requires authentication)
- Delivery uses the configured notifier — see [Email Delivery](#email-delivery)

### Email Delivery

Password resets and email verification go through a configurable notifier with three possible channels:

| Channel | When to use | Requirements |
|---|---|---|
| **SMTP** | You have an existing mail server | `SMTP_HOST` + `SMTP_FROM` set in config |
| **Webhook** | You want delivery via Slack / Postmark / SendGrid / a custom relay | At least one enabled webhook subscribed to `auth.password_reset_requested` and/or `auth.email_verification_requested` |
| **Disabled** | Local dev / pre-launch staging | Nothing — tokens are logged to server output |

**Pick the channel** in **Admin → Settings → Operations → Email Notifier**. The default is `auto`: SMTP if configured, else webhook if an `auth.*`-subscribed webhook exists, else disabled.

**Check the active channel** on `GET /health` — the `notifier` field returns `smtp`, `webhook`, or `noop`. If it returns `noop` in production, server logs will contain an ERROR at boot:

```
notifier is NOOP — password reset and email verification will not be delivered.
Configure SMTP, or enable a webhook subscribed to auth.* events, or set notifier_method explicitly.
```

#### Webhook payload

When the webhook channel is active, each event POSTs JSON to every subscribed webhook URL with the standard signature pipeline (HMAC-SHA256, 3 retries, exponential backoff):

```json
{
  "event_type": "auth.password_reset_requested",
  "timestamp": "2026-05-18T12:00:00Z",
  "auth": {
    "to": "alice@example.com",
    "url": "https://indelible.example.com/reset-password?token=abc123..."
  }
}
```

Your webhook handler is responsible for delivering `auth.url` to `auth.to` through whatever channel makes sense (transactional email API, Slack DM, Discord webhook, an internal mail queue). The `X-Signature-256` header is `sha256=<hmac>` over `<X-Webhook-Timestamp>.<raw body>` using the webhook secret — see [Webhook Signatures](#webhook-signatures).

Same shape applies to `auth.email_verification_requested`.

### Changing Password

Go to **Profile** (click your name in the sidebar) to change your password. You must provide your current password.

---

## File Uploads

### Uploading Files

**Via Dashboard:**
1. Go to the Dashboard (`/`)
2. Select a file, choose visibility (public or private), and click Upload
3. The file is queued for upload to the Autonomi network

**Via API:**
```bash
curl -X POST https://files.acme.com/api/v2/uploads \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@document.pdf" \
  -F "visibility=private"
```

### Upload Status

Files go through these stages:
- **Queued** — received and waiting for background processing
- **Processing** — being uploaded to the Autonomi network
- **Completed** — available for download with a permanent network address
- **Failed** — error occurred (check error message)

### Downloading Files

Completed files can be downloaded from the Uploads page or via API:

```bash
curl -O -J https://files.acme.com/api/v2/uploads/{uuid}/download \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Visibility

- **Private** (default): only the uploader can access the file
- **Public**: stored as public data on the Autonomi network

### Cost Quote

Get an exact cost quote before uploading by sending the actual file. antd runs self-encryption on the bytes and queries the live network for chunk pricing — no estimation, no scaling.

```bash
curl -X POST https://files.acme.com/api/v2/uploads/quote \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@document.pdf" \
  -F "visibility=private"
```

The response includes a structured `estimated_cost` breakdown:

```json
{
  "estimated_cost": {
    "cost": "123456789",
    "file_size": 1048576,
    "chunk_count": 4,
    "estimated_gas_cost_wei": "45000000000",
    "payment_mode": "auto"
  },
  "file_size": 1048576,
  "original_filename": "document.pdf",
  "visibility": "private"
}
```

- `cost` — storage cost in atto tokens
- `estimated_gas_cost_wei` — advisory gas heuristic, not a live gas-oracle quote
- `payment_mode` — `auto`, `merkle`, or `single`

---

## Collections & Tags

### Tags

**Set tags at upload time** (recommended for programmatic workflows):

```bash
curl -X POST https://files.acme.com/api/v2/uploads \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@document.pdf" \
  -F 'visibility=private' \
  -F 'tags={"department":"legal","project":"alpha","client":"acme"}'
```

**Set/update tags after upload** (replace-all semantics):

```bash
curl -X PUT https://files.acme.com/api/v2/uploads/{uuid}/tags \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tags": {"department": "legal", "project": "alpha"}}'
```

**Auto-tag rules** — Admins can create rules that automatically apply tags at upload time (Admin > Tag Rules). Rules match on content type, filename regex, file size, or visibility.

### Searching by Tags

**Label selector syntax** (recommended — supports equality, inequality, set membership, existence):

```bash
# Equality
curl "https://files.acme.com/api/v2/tags/search?selector=department=legal" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Inequality
curl "https://files.acme.com/api/v2/tags/search?selector=status!=archived" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Set membership
curl "https://files.acme.com/api/v2/tags/search?selector=env in (prod,staging)" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Key exists / not exists
curl "https://files.acme.com/api/v2/tags/search?selector=reviewed" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Combined with filename search
curl "https://files.acme.com/api/v2/tags/search?selector=department=legal,status!=archived&q=contract" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**Legacy syntax** (backward compatible):

```bash
curl "https://files.acme.com/api/v2/tags/search?tag.department=legal&q=contract" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Bulk Tag Operations

Apply or remove tags across multiple files at once:

```bash
# By UUID list
curl -X POST https://files.acme.com/api/v2/tags/bulk \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"upload_uuids": ["uuid1", "uuid2"], "add_tags": {"archived": "true"}}'

# By selector (tag all completed legal files as reviewed)
curl -X POST https://files.acme.com/api/v2/tags/bulk \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"selector": "department=legal,status=completed", "add_tags": {"reviewed": "true"}}'
```

### Tag Facets

Get aggregated tag counts for building filter UIs:

```bash
curl "https://files.acme.com/api/v2/tags/facets" \
  -H "Authorization: Bearer YOUR_TOKEN"
# Returns: {"facets": [{"key":"department","value":"legal","count":42}, ...]}
```

### Collections

Collections are virtual folders for organising files. Create them from the Collections page or via API:

```bash
# Create collection
curl -X POST https://files.acme.com/api/v2/collections \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Q1 Tax Docs", "description": "Tax documents for Q1 2026"}'

# Add file to collection (use upload UUID)
curl -X POST https://files.acme.com/api/v2/collections/{id}/files \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"upload_uuid": "abc-123-..."}'
```

Collections support hierarchical nesting via `parent_id`. Deleting a parent collection cascades to all children.

---

## API Tokens

API tokens provide programmatic access for CI/CD pipelines, integrations, and automation.

### Creating Tokens

1. Go to **API Tokens** in the sidebar
2. Click **New Token**
3. Set a name, scope (read/write/admin), and expiry
4. **Copy the token immediately** — it's shown only once

**Via API:**
```bash
curl -X POST https://files.acme.com/api/v2/tokens \
  -H "Authorization: Bearer YOUR_JWT" \
  -H "Content-Type: application/json" \
  -d '{"name": "CI Pipeline", "scope": "write", "expires_in_days": 90}'
```

### Token Scopes

- **read** — list and download files, view uploads
- **write** — upload files, manage tags and collections
- **admin** — full access including user management (only admins can create admin tokens)

### Using Tokens

Pass the token in the Authorization header:

```bash
curl -H "Authorization: Bearer ind_abc123..." https://files.acme.com/api/v2/uploads
```

### Revoking Tokens

Tokens can be revoked from the UI or API. Revocation is permanent — create a new token instead.

---

## Admin: User Management

*Requires admin permissions.*

### Permissions Model

Three levels: **read**, **write**, **admin**. Admin implies write implies read.

- **Direct permissions** — set per user by admins
- **Group permissions** — inherited from group membership
- **Effective permissions** = union of direct + group

The system prevents removing the last admin.

### Service Accounts

Non-human accounts for CI pipelines and integrations:

- No password, no login — exist only to own API tokens
- Created by admins from the Users page
- Tokens survive employee departures

### Groups

Groups grant a permission level to all members. Use groups for department-level access control.

---

## Admin: Wallets

*Requires admin permissions.*

Wallets store Autonomi network credentials for paying upload costs.

### Setup

1. Go to **Admin > Wallets**
2. Click **Add Wallet**
3. Enter the wallet name, address, and private key
4. The first wallet is automatically set as default

**Important:** The private key is encrypted at rest using AES-256-GCM. Ensure `INDELIBLE_WALLET_ENCRYPTION_KEY` is set to a secure value.

### Default Wallet

The default wallet is used for all upload payments. Click **Set Default** on any wallet to change it.

---

## Admin: Storage Quotas

*Requires admin permissions. Quotas are disabled by default.*

Quotas prevent runaway storage costs by limiting total upload volume.

### Quota Types

| Entity Type | Description |
|------------|-------------|
| `system` | Global limit across all users |
| `user` | Per-user limit (entity_id = user ID) |
| `group` | Per-group limit |
| `department` | Per-department limit |

### Creating Quotas

1. Go to **Admin > Quotas**
2. Click **New Quota**
3. Select entity type, optional entity ID, and max storage in GB

### Enforcement

When a quota is enabled, uploads are rejected if they would exceed the limit. The usage bar on the Quotas page shows current consumption.

Quota usage is calculated from the total size of **completed** uploads.

---

## Admin: System Settings

*Requires admin permissions.*

Runtime settings are stored in the database and take effect immediately without restart.

### Available Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `environment_name` | `production` | Environment label shown in UI |
| `timezone` | `UTC` | System timezone (IANA format) |
| `max_upload_size_bytes` | `10737418240` (10 GB) | Maximum file size for uploads |
| `default_visibility` | `private` | Default upload visibility |
| `retention_days` | `30` | Log retention period |
| `maintenance_mode` | `false` | Enable maintenance mode |
| `maintenance_message` | — | Custom maintenance message |
| `jwt_expiry_hours` | `24` | JWT session lifetime |
| `default_token_expiry_days` | `90` | Default API token expiry |
| `max_concurrent_uploads` | `1` | Background upload concurrency |
| `log_retention_enabled` | `false` | Enable automatic log cleanup |
| `log_retention_days` | `30` | Days to keep non-audit logs |
| `antd_quote_timeout_secs` | `300` | Per-request timeout (seconds) for `POST /uploads/quote` calling antd. Bounds: **1–3600**. |
| `antd_health_probe_timeout_secs` | `15` | Per-call timeout (seconds) for the antd-connectivity probe used by `GET /health` and the system-monitor alert loop. Bounds: **1–120**. |

### Tuning antd timeouts

The two `antd_*_timeout_secs` settings bound how long indelible waits on the antd daemon before giving up. They take effect within the 30s settings cache TTL — no restart required.

- **`antd_quote_timeout_secs`** — defaults to 300 because antd's first quote after startup waits for peer-bootstrap on the Autonomi DHT, which can take 2–3 minutes on mainnet. Once antd is warm, real quotes return in single-digit seconds. Lower this if you'd rather have quotes fail fast (502) than block the request; raise it only if you see legitimate timeouts after the warm-up window.
- **`antd_health_probe_timeout_secs`** — defaults to 15 and bounds the `DataCost([0,0,0])` probe both `GET /health` and the system-monitor alert loop use to declare antd "down". Tightening it makes recovery alerts fire faster but flaps more on transient slow networks; loosening it hides genuine degradation for longer.

These are independent of antd's own `--quote-timeout-secs` / `--store-timeout-secs` flags (defaults 10s each), which bound *per-chunk* network calls inside the antd daemon. Indelible's settings here bound the *whole HTTP request* it makes to antd — so they need to be larger than the daemon's per-chunk timeout × expected number of parallel batches × any cold-bootstrap window.

Invalid values (negative, non-numeric, out of bounds) are rejected by `PATCH /api/v2/admin/settings` with HTTP 400. If a bad value somehow lands in the DB out-of-band, the read sites clamp to the default and emit a `WARN` log.

### Export / Import

Instance configuration can be exported as JSON for backup or replication to another instance:

1. Go to **Admin > Settings**
2. Click **Export** to download `indelible-settings.json`
3. On another instance, click **Import** and select the file

The export includes:
- **System settings** — all runtime configuration (maintenance mode, upload limits, log retention, etc.)
- **Webhook configurations** — URLs, integration types, event subscriptions, enabled state
- **OIDC provider configurations** — names, issuer URLs, client IDs, scopes (client secrets are excluded for security — re-enter after import)
- **Group definitions** — names, descriptions, permission levels

The import is backwards-compatible with older flat settings exports. All settings changes are recorded in the config audit trail.

---

## Admin: Webhooks

*Requires admin permissions.*

Webhooks send HTTP notifications when events occur — upload status changes and system alerts. Navigate to **Admin > Webhooks** in the sidebar.

### Creating a Webhook

1. Click **Add Endpoint**
2. Enter the endpoint URL
3. Choose integration type: **Generic JSON** or **Slack**
4. Select which events to subscribe to:
   - **Upload events:** `queued`, `processing`, `completed`, `failed`
   - **System events:** `disk_warning`, `disk_critical`, `disk_recovered`
5. Click **Create Webhook**

### Managing Webhooks

- **Enable/Disable** — toggle in the list without deleting
- **Edit** — click the pencil icon to expand an inline edit panel (URL, type, events, enabled)
- **Test** — click the lightning bolt icon to send a test ping; result shown inline
- **History** — click the clock icon to view recent delivery attempts with status codes and errors
- **Delete** — click the trash icon (requires confirmation)

### Integration Types

- **Generic** — raw JSON payload, suitable for any HTTP endpoint
- **Slack** — formatted Block Kit message for Slack incoming webhooks

### Event Types

| Event | Category | When it fires |
|-------|----------|---------------|
| `queued` | Upload | File accepted and queued for processing |
| `processing` | Upload | Worker started processing the upload |
| `completed` | Upload | Upload successfully stored on the network |
| `failed` | Upload | Upload processing failed |
| `disk_warning` | System | Disk usage reached 80% |
| `disk_critical` | System | Disk usage reached 95%, uploads paused |
| `disk_recovered` | System | Disk usage returned below warning threshold |

System alerts fire once per threshold transition — not every check interval (5 minutes).

### Delivery

- Async and non-blocking to the upload flow
- 3 retry attempts with exponential backoff
- 5-second timeout per attempt
- Every delivery attempt is logged and visible in the delivery history panel

### Payload Format (Generic — Upload Event)

```json
{
  "event_type": "completed",
  "timestamp": "2026-03-13T12:00:00Z",
  "upload": {
    "uuid": "abc-123-...",
    "user_id": 5,
    "token_id": 12,
    "filename": "document.pdf",
    "status": "completed",
    "file_size": 1048576,
    "visibility": "private",
    "actual_cost": "5000000"
  }
}
```

`user_id` is always present. `token_id` is included when the upload was created via API token — this allows API consumers to filter webhooks for their own uploads without needing per-user webhook configuration.

### Payload Format (Generic — System Event)

```json
{
  "event_type": "disk_critical",
  "timestamp": "2026-03-13T12:00:00Z",
  "system": {
    "alert_type": "disk_critical",
    "message": "Disk usage at 96.2%, uploads paused",
    "value": 96.2
  }
}
```

### Payload Format (Test Ping)

```json
{
  "event_type": "test_ping",
  "timestamp": "2026-03-13T12:00:00Z"
}
```

---

## Admin: OIDC / SSO Providers

*Requires admin permissions.* Indelible supports OpenID Connect single sign-on with identity providers like Okta, Microsoft Entra ID (Azure AD), Google, and Keycloak.

➡️ See the dedicated **[SSO setup guide](docs/guides/sso.md)** for configuring providers (Admin → SSO → **+ Add provider**). For full per-IdP walkthroughs, see [Provisioning with Okta](#provisioning-with-okta) and [Provisioning with Azure AD](#provisioning-with-azure-ad) below.

---

## Admin: SCIM Provisioning

*Requires admin permissions.* SCIM 2.0 auto-provisions users and groups from identity providers such as Okta, Microsoft Entra ID (Azure AD), and Google Workspace.

➡️ See the dedicated **[SCIM setup guide](docs/guides/scim.md)** for enabling SCIM, generating tokens, IdP configuration, and attribute mapping. For full per-IdP walkthroughs, see [Provisioning with Okta](#provisioning-with-okta) and [Provisioning with Azure AD](#provisioning-with-azure-ad) below.

---

## Provisioning with Okta

End-to-end walkthrough for connecting Indelible to an Okta tenant. Covers two integrations that work together but can be deployed independently:

- **SSO (OIDC)** — users sign in to Indelible with their Okta credentials.
- **SCIM** — Okta provisions, updates, and deactivates users in Indelible automatically as assignments change.

Most enterprise deployments use both. SSO handles authentication; SCIM handles the user-and-group lifecycle. The two are linked automatically: when SCIM creates a user with an `externalId` that matches an SSO `sub` claim from the same tenant, the SSO login finds the SCIM-provisioned account.

### Prerequisites

- Admin access to an Okta tenant (developer or production).
- A publicly reachable URL for Indelible. During evaluation, [`cloudflared`](https://github.com/cloudflare/cloudflared) tunnels work well: `cloudflared tunnel --url http://localhost:8080` returns an ephemeral `https://*.trycloudflare.com` URL Okta can call back to. For production, a stable hostname behind a reverse proxy is required.
- An Indelible admin account (the first registered user is auto-promoted to admin).

> Throughout this section, `https://your-indelible` stands in for whatever public URL Indelible is reachable at. Replace it with your tunnel URL or production hostname.

---

### Part A — Single Sign-On (OIDC)

#### 1. Create an Okta OIDC application

In the Okta admin console:

1. **Applications → Applications → Create App Integration**.
2. Sign-in method: **OIDC — OpenID Connect**. Application type: **Web Application**. Click **Next**.
3. Name the integration (for example, `Indelible`).
4. **Sign-in redirect URIs**: add `https://your-indelible/api/v2/auth/oidc/callback`.
5. **Sign-out redirect URIs**: leave blank (Indelible handles logout locally in v1).
6. **Assignments**: choose **Skip group assignment for now**. You will assign yourself explicitly in the next step.
7. Save. On the resulting screen, copy the **Client ID** and **Client Secret** — you will paste them into Indelible shortly.

<!-- screenshot: Okta "Create a new app integration" dialog with OIDC + Web Application selected -->

<!-- screenshot: Okta application General tab showing Client ID and Client Secret fields -->

#### 2. Assign yourself to the application

Okta's "Allow everyone in your organization to access" toggle is frequently a no-op on developer/integrator tenants — even tenant admins must be explicitly assigned. To avoid a confusing `403` later:

1. Open the application → **Assignments** tab.
2. **Assign → Assign to People** → select your own account → **Save and Go Back**.

<!-- screenshot: Okta Assignments tab with an explicitly-assigned user row -->

#### 3. Note the issuer URL

Okta exposes two authorization servers; pick the right one:

- **Org authorization server** — `https://<tenant>.okta.com` (no trailing path). Simpler; no per-client access policy. **Recommended for first-time integrations.**
- **Default custom authorization server** — `https://<tenant>.okta.com/oauth2/default`. Requires you to add the new client to the default access policy under **Security → API → Authorization Servers**, otherwise the token exchange fails with `Policy evaluation failed`.

For the remainder of this walkthrough, the org auth server form is used.

#### 4. Add the provider in Indelible

1. Log into Indelible as an admin.
2. Navigate to **Admin → SSO** (the dedicated provider-management page at `/admin/sso`).
3. Click **Add provider** and fill in:

| Field | Value |
|---|---|
| Name | `okta` (internal identifier; lowercase, no spaces) |
| Display name | `Sign in with Okta` (appears on the login page) |
| Issuer URL | `https://<tenant>.okta.com` |
| Client ID | from step 1 |
| Client secret | from step 1 |
| Scopes | `openid email profile` (the default) |

The client secret is encrypted at rest with AES-256-GCM.

<!-- screenshot: Indelible Admin → SSO "Add provider" form filled in -->

4. Save the provider. It appears in the list with a status indicator.

#### 5. Configure provisioning behavior

For each saved provider you can tune four behaviors from the same screen:

- **Auto-provision** — when on, an unknown but verified Okta user signing in for the first time becomes an Indelible user automatically. When off, they get an "account not found" error and an admin must create the user first.
- **Default group** — when auto-provision is on, new users join this group. The group's permission level determines what they can do on first login.
- **Require verified email** — when on, only Okta users whose ID token carries `"email_verified": true` are accepted. When off, the email-verification claim is not required.
- **Extra authorize params** — key/value pairs appended to the authorize URL. Useful for vendor-specific options like `prompt=login` or `hd=<workspace-domain>`.

**Important: the "Require verified email" default is on (strict).** Okta developer/integrator tenants frequently do not emit the `email_verified` claim at all (the default custom authorization server reserves `email` and `email_verified` as protected claim names — you cannot add them manually, and the org auth server omits the field on integrator tenants). With the strict default, auto-provision will refuse with `did not return a verified email` even though the user is real.

For Okta developer/integrator tenants, **turn "Require verified email" off** on the Okta provider. For production Okta tenants that emit the claim correctly, leave it on.

<!-- screenshot: Provider settings panel showing the four toggles -->

#### 6. Test sign-in

1. Log out of Indelible.
2. On the login page, click the **Sign in with Okta** button.
3. You should be redirected to Okta, see the standard sign-in screen, then land on the Indelible dashboard.

If you get bounced back to the login page silently, check the network tab — a `/api/v2/auth/oidc/callback` returning `200` followed by an immediate `/login` redirect indicates the cookie was set but the SPA could not read it. This was fixed in recent releases; ensure you are on the latest version.

#### 7. Manually link an existing local account (optional)

If you already have a local Indelible account and want to add Okta sign-in to it (instead of creating a fresh SSO user):

1. Sign in with your local password.
2. **Profile → Connected Accounts → Connect Okta**.
3. You are redirected through Okta; on return the two accounts are linked. You can now sign in with either credential.

> Indelible never auto-links by email address alone — if an SSO email matches an existing local user but no explicit link exists, sign-in is refused. This is a deliberate safeguard against an attacker registering an Okta account at the same email to take over a local one.

### Part B — SCIM provisioning

SCIM and SSO are independent — you can run SCIM without SSO (users get provisioned but have no way to sign in), or SSO without SCIM (users self-provision via auto-provision on first sign-in). They are most useful together.

#### 1. Enable SCIM in Indelible

1. Navigate to **Admin → SCIM** (`/admin/scim`).
2. Toggle **SCIM provisioning** to enabled. The page now displays the **SCIM base URL** for your deployment, e.g. `https://your-indelible/scim/v2`.

<!-- screenshot: Indelible Admin → SCIM page with provisioning enabled and the base URL visible -->

#### 2. Mint a SCIM token

1. Click **Generate token**.
2. Give it a descriptive name (for example, `Okta Production`).
3. **Copy the token immediately** — it is shown only once. Tokens have the form `scim_<64-hex-chars>`.

#### 3. Add the SCIM 2.0 Test App in Okta

1. Okta admin → **Applications → Browse App Catalog**.
2. Search for **SCIM 2.0 Test App (Header Auth)** and select it.
3. Click **Add Integration**.

<!-- screenshot: Okta App Catalog tile for "SCIM 2.0 Test App (Header Auth)" -->

4. Give the integration a name (for example, `Indelible SCIM`). Accept the defaults on the **General Settings** step and click **Next**, then **Done**.

#### 4. Save before enabling provisioning

A non-obvious Okta quirk: the **Provisioning** tab does not appear until SCIM has been enabled and the page has been saved. On the application's **General** tab:

1. Locate the **Provisioning** section (or **App Settings → Provisioning**) and switch it to **SCIM**.
2. Click **Save**. The **Provisioning** tab now appears in the top nav.

<!-- screenshot: Okta application General tab with Provisioning set to SCIM -->

#### 5. Sign-on Options

On the **Sign On** tab:

- SAML / SWA fields can be left blank — SCIM uses Header Auth, not SAML.
- Under **Credential Details**, set **Application username format** to **Okta username**.
- Save.

<!-- screenshot: Okta Sign On Options page with Application username format = Okta username -->

#### 6. Connect Okta to Indelible's SCIM endpoint

On the **Provisioning → Integration** sub-tab → **Edit**:

| Field | Value |
|---|---|
| SCIM connector base URL | `https://your-indelible/scim/v2` |
| Unique identifier field for users | `userName` |
| Supported provisioning actions | check **Push New Users**, **Push Profile Updates**, **Push Groups** |
| Authentication Mode | **HTTP Header** |
| Authorization (header) | `scim_<your-token>` |

> The Authorization field is passed verbatim by the Header Auth variant — Okta does not prepend `Bearer `. Indelible's SCIM middleware accepts both `Bearer scim_<token>` and bare `scim_<token>`, so the simplest form (just the token) works. Older documentation may instruct you to type `Bearer scim_<token>` literally; that still works but is no longer required.

Click **Test API Credentials** → expect a green confirmation banner.

<!-- screenshot: Okta SCIM Integration config screen filled in -->

<!-- screenshot: Okta "Test API Credentials" green success banner -->

Save.

#### 7. Enable provisioning actions

On the **Provisioning → To App** sub-tab → **Edit**, enable:

- **Create Users**
- **Update User Attributes**
- **Deactivate Users**

Save.

<!-- screenshot: Okta Provisioning → To App with Create / Update / Deactivate all enabled -->

#### 8. Assign a user

On the **Assignments** tab:

1. **Assign → Assign to People** → pick a user → **Save and Go Back** → **Done**.
2. Okta first sends a `GET /Users?filter=userName eq "..."` existence check, then a `POST /Users` if no match is found.
3. The user should appear in Indelible's **Admin → Users** within seconds (visible refresh: SCIM events show up in the user list immediately).

<!-- screenshot: Indelible Admin → Users list showing the newly SCIM-provisioned user -->

> **SCIM-provisioned users have no password.** Okta includes an initial random password in the `POST /Users` payload; Indelible ignores it. SCIM users must sign in via SSO (Part A) or have an admin reset their password manually.

#### 9. Push a group

On the **Push Groups** tab:

1. **Push Groups → Find groups by name** (avoid **Find groups by rule** for evaluation).
2. Type the name of an Okta group, select it, choose **Create Group** (not **Link Group** — use Link only if the group already exists in Indelible and you want to map them).
3. Save. Okta sends a `POST /Groups` followed by `PATCH /Groups/{id}` for each member.
4. The group appears in **Admin → Groups** in Indelible with members populated.

<!-- screenshot: Okta Push Groups tab with one linked group -->

> SCIM-provisioned groups default to **read** permission level. To grant more, edit the group in Indelible after provisioning — the permission level is local-only and is not overwritten by subsequent SCIM updates.

#### 10. Deactivation

When an Okta user is unassigned from the application (or their Okta account is deactivated), Okta sends `PATCH /Users/{id}` with `active: false`. Indelible soft-deletes the user — they can no longer sign in, but their audit history is preserved. Re-assigning them in Okta restores access.

### Combined SSO + SCIM

Run both Part A and Part B against the same Okta tenant. The linkage happens automatically: SCIM stores the Okta user ID as `externalId`; when the user signs in via SSO, the OIDC `sub` claim is matched against `externalId` and the existing user is reused (no duplicate created).

For this auto-link to work, both integrations must be configured against the same Okta tenant — `sub` and SCIM `id` are only globally meaningful within a single tenant.

### Troubleshooting

| Symptom | Likely cause |
|---|---|
| `Policy evaluation failed` during SSO | Using the default custom authorization server without adding the new client to the access policy. Switch to the org auth server, or edit **Security → API → Authorization Servers → default → Access Policies**. |
| `did not return a verified email` on first sign-in | Okta integrator tenants omit `email_verified`. Turn off **Require verified email** on the provider. |
| SSO returns `Sign-in failed unexpectedly` after deleting a user | Resolved in recent releases — ensure you are on the latest version. Symptom was a soft-deleted user blocking the email or OIDC-identity slot. |
| SCIM `401 invalid authorization format` | Token is mistyped or missing. The Authorization field should contain either `scim_<hex>` or `Bearer scim_<hex>` — anything else (no scheme, wrong prefix) is rejected. |
| Okta keeps retrying an old base URL after you change it | Okta's SCIM endpoint config caches. Wait a few minutes, or disable and re-enable provisioning on the Okta side to force a refresh. |
| Sign-in succeeds in incognito but auto-picks an account in your regular browser | Okta vendor behavior — `prompt=select_account` is honored only when no active session exists. Use `prompt=login` in **Extra authorize params** for QA flows where you need re-authentication every time. |

---

## Provisioning with Azure AD

A full Azure AD / Entra walkthrough mirrors the Okta one above but is **not yet validated end-to-end against a real tenant** — the rehearsal is planned but currently blocked on test-tenant access. Until that rehearsal completes, the following is a sketch of the configuration shape. Use it as a starting point but expect minor field-name differences from current Azure portal copy.

### SSO (OIDC)

1. **Azure Portal → App registrations → New registration**.
2. Redirect URI: **Web** → `https://your-indelible/api/v2/auth/oidc/callback`.
3. After registration, capture the **Application (client) ID** and the **Directory (tenant) ID**. The issuer URL is `https://login.microsoftonline.com/<tenant-id>/v2.0`.
4. **Certificates & secrets → New client secret** → copy the secret **value** (not the ID) immediately.
5. **API permissions → Microsoft Graph → Delegated → openid, email, profile** → grant admin consent.
6. In Indelible: **Admin → SSO → Add provider** with the values above. Scopes: `openid email profile`.
7. Azure AD reliably emits `email_verified`, so leave **Require verified email** on.

### SCIM

1. **Azure Portal → Enterprise applications → New application → Create your own → Integrate any other application (non-gallery)**.
2. Open the application → **Provisioning → Get started**.
3. **Provisioning Mode**: Automatic.
4. **Tenant URL**: `https://your-indelible/scim/v2`.
5. **Secret Token**: `scim_<your-token>` (Azure AD accepts the bare form).
6. **Test Connection** → expect success → **Save**.
7. **Provisioning → Start provisioning**.

Azure AD-specific behaviors to expect (validated indirectly via Okta captures, but not yet against a real Azure tenant):

- Azure issues `GET /Users?filter=userName eq "..."` before every `POST /Users` as an existence check.
- Group membership removal uses the `members[value eq "X"]` filter form in `PATCH /Groups/{id}` payloads.
- The provisioning cycle runs every **40 minutes by default**. Use the **Provision on demand** button on the Provisioning page to force an immediate sync during testing.

This section will be expanded with screenshots and a confirmed step list once the Azure AD rehearsal completes.

---

## Admin: Analytics

*Requires admin permissions.*

### Upload Analytics

View upload volume, success/failure rates, average processing time, and top uploaders. Filter by time period (7, 30, or 90 days).

### Token Usage

See total API requests, active token count, and most active tokens.

### Cost Analytics

Track storage costs:
- **System-wide** — total transactions and spend
- **By department** — cost allocation across teams
- **By token** — identify high-usage integrations

---

## Admin: Logs

*Requires admin permissions.*

Log tiers:

### Audit Logs

Security events: logins, permission changes, config changes, token operations, and file uploads/deletes. Hash-chained and tamper-evident. **Never deleted** (compliance). Filterable by event type, user, and date range.

### File Access Logs

File-read telemetry: `file_downloaded` and `file_download_denied`. Kept in a separate plain, append-only table (not the audit hash-chain) so it can absorb high download volume across multiple instances. Filterable and exportable like the audit log. **Never deleted.**

> File **downloads** are recorded here, not in the Audit log. File uploads and deletes remain in the Audit log.

### System Logs

Internal operations: worker events, webhook delivery, errors. Searchable by level (info/warn/error) and component. Subject to log retention settings.

### User Activity Logs

User actions: logins, uploads, tag changes. Filterable by user and date range. Subject to log retention settings. (File downloads appear in the File Access log above.)

### Log Retention

To enable automatic cleanup of system and user logs:

1. Go to **Admin > Settings**
2. Set `log_retention_enabled` to `true`
3. Set `log_retention_days` to your desired period (1–365)

Audit logs are **exempt** from retention and are kept permanently.

---

## Maintenance Mode

When enabled, all API endpoints (except `/health` and admin routes) return **503 Service Unavailable** with a custom message.

### Enabling

1. Go to **Admin > Settings**
2. Set `maintenance_mode` to `true`
3. Optionally set `maintenance_message` to a custom message

### Via API

```bash
curl -X PATCH https://files.acme.com/api/v2/admin/settings \
  -H "Authorization: Bearer ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"maintenance_mode": "true", "maintenance_message": "Upgrading — back in 30 minutes"}'
```

---

## Rate Limiting

Three endpoints have per-key rate limits today. Hit the limit and you get a `429 Too Many Requests` with a `Retry-After` header (seconds until the window resets).

| Endpoint | Limit | Window | Keyed by |
|---|---|---|---|
| `POST /api/v2/auth/login` | 5 attempts | 60 s | client IP |
| `POST /api/v2/auth/forgot-password` and `/auth/reset-password` | 3 attempts | 60 s | client IP |
| `POST /api/v2/uploads` | 60 requests | 60 s | authenticated user ID |

The IP-keyed limiters honour `X-Forwarded-For` only when the connection comes from a CIDR listed in `trusted_proxies` (prevents header spoofing). All other endpoints are currently unrate-limited — protect them with your reverse proxy or WAF if you need broader throttling.

---

## Disk Space Monitoring

Indelible monitors the data directory for disk usage every 5 minutes:

| Threshold | Action |
|-----------|--------|
| **80%** | Warning logged to system logs |
| **95%** | Critical alert, new uploads paused |
| Below 80% | Uploads automatically resume |

The health endpoint (`/health`) reflects the paused state. This is essential because the upload queue writes files to local disk before uploading to the Autonomi network.

---

## API Consumer Guide

### Request ID Tracing

Every API response includes an `X-Request-Id` header. Include this ID in support requests to help correlate issues with server-side logs.

### Rate Limiting

Rate-limited endpoints (login, password-reset, uploads — see [Rate Limiting](#rate-limiting) above for the matrix) include these headers on every response:

| Header | Description |
|--------|-------------|
| `X-RateLimit-Limit` | Maximum requests per window |
| `X-RateLimit-Remaining` | Requests remaining in current window |
| `X-RateLimit-Reset` | Unix timestamp when the window resets |
| `Retry-After` | Seconds until retry is allowed (only on 429) |

### Error Codes

Error responses include a machine-readable `code` field alongside the human-readable `error` message:

```json
{"error": "invalid email or password", "code": "invalid_credentials"}
```

Common codes: `unauthorized`, `forbidden`, `validation_error`, `not_found`, `invalid_credentials`, `quota_exceeded`, `file_too_large`, `rate_limit_exceeded`, `maintenance_mode`, `wallet_not_configured`.

### Idempotency Keys

For upload creation (`POST /uploads`), include an `Idempotency-Key` header to safely retry requests:

```bash
curl -X POST /api/v2/uploads \
  -H "Idempotency-Key: unique-request-id-123" \
  -F file=@document.pdf
```

If the same key + user combination is sent again within 24 hours, the original response is replayed with `X-Idempotent-Replayed: true`.

### Webhook Signatures

All webhook deliveries include HMAC-SHA256 signatures for payload verification:

| Header | Description |
|--------|-------------|
| `X-Signature-256` | `sha256=<hex-encoded HMAC>` over `<timestamp>.<raw body>` |
| `X-Webhook-Timestamp` | Unix timestamp of delivery — **signed into** the signature |

The signature is computed over the timestamp joined to the raw body
(`<X-Webhook-Timestamp>.<body>`), so a receiver can bind it to a time window and
reject replays. To verify in your receiver:
```python
import hmac, hashlib, time
ts = request.headers["X-Webhook-Timestamp"]
signed = ts.encode() + b"." + body  # body = the raw request bytes
expected = "sha256=" + hmac.new(secret.encode(), signed, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, request.headers["X-Signature-256"])
# Reject stale deliveries (recommended tolerance: 5 minutes).
assert abs(time.time() - int(ts)) <= 300
```

> **Signature scheme change:** earlier releases signed the body only. Receivers
> that verified the old scheme must update to sign `<timestamp>.<body>`.

Webhook secrets are generated automatically on creation and shown once. Use the **Rotate Secret** button to generate a new one.

### Upload Filtering and Sorting

`GET /uploads` supports these query parameters:

| Parameter | Example | Description |
|-----------|---------|-------------|
| `status` | `completed` | Filter by upload status |
| `sort` | `file_size:asc` | Sort by field with direction |
| `from` | `2026-01-01T00:00:00Z` | Created after (RFC 3339) |
| `to` | `2026-03-01T00:00:00Z` | Created before (RFC 3339) |

Sort fields: `created_at` (default), `file_size`, `filename`, `status`.

### Cursor Pagination

List endpoints support cursor-based pagination as an alternative to offset/limit:

```bash
# First page (offset/limit — includes next_cursor in response)
GET /api/v2/uploads?limit=10

# Next page using cursor
GET /api/v2/uploads?cursor=<next_cursor_value>&limit=10
```

The response includes `next_cursor` and `prev_cursor` fields when applicable.

---

## Deployment

### Minimal (Single Binary + SQLite)

```bash
export INDELIBLE_JWT_SECRET="$(openssl rand -hex 32)"
export INDELIBLE_ANTD_URL="http://localhost:8082"
./indelible
```

### Production (Behind Reverse Proxy)

**Caddy** (simplest — automatic Let's Encrypt):
```
files.acme.com {
    reverse_proxy localhost:8080
}
```

**Nginx:**
```nginx
server {
    listen 443 ssl;
    server_name files.acme.com;
    ssl_certificate     /etc/ssl/files.acme.com.crt;
    ssl_certificate_key /etc/ssl/files.acme.com.key;
    client_max_body_size 10g;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Docker Compose

The repo ships a canonical [`docker-compose.yml`](./docker-compose.yml) at the root with `indelible` + `antd` + Postgres pre-wired. To run the stack:

```bash
export INDELIBLE_JWT_SECRET=$(openssl rand -hex 32)
export INDELIBLE_WALLET_ENCRYPTION_KEY=$(openssl rand -hex 32)
docker compose up -d
```

The compose file's volumes mount data at the container's `/var/lib/indelible` (matches the in-image default). Add `CORS_ORIGINS`, trusted-proxy ranges, or SMTP credentials by extending the `environment:` block — see the comments at the top of the file for the full list. A simpler SQLite-only variant (no Postgres service) is in the comment block at the bottom of `docker-compose.yml`.

To build from local source instead of pulling a published image:

```bash
docker compose up --build
```

### Trusted Proxies

When behind a reverse proxy, configure trusted proxy ranges so Indelible correctly reads client IPs from `X-Forwarded-For`. Without this, `X-Forwarded-For` is ignored and the direct connection IP is used (safe default). This affects rate limiting and audit log IP addresses.

```toml
trusted_proxies = ["127.0.0.1/32", "10.0.0.0/8"]
```

Or via environment variable:
```bash
export INDELIBLE_TRUSTED_PROXIES="127.0.0.1/32,10.0.0.0/8"
```

---

## API Reference

All endpoints are under `/api/v2/`. Authentication is via Bearer token (JWT or API token).

### Public Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/login` | Login (rate-limited) |
| POST | `/auth/register` | Register new user |
| POST | `/auth/forgot-password` | Request password reset |
| POST | `/auth/reset-password` | Reset password with token |
| GET | `/auth/verify-email` | Verify email with token |
| GET | `/health` | Health check |

### User Endpoints (authenticated)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/me` | Get profile |
| PUT | `/me` | Update profile |
| PUT | `/me/password` | Change password |
| POST | `/me/resend-verification` | Resend verification email |
| POST | `/uploads` | Upload file (multipart) |
| GET | `/uploads` | List uploads |
| GET | `/uploads/{id}` | Get upload by UUID |
| POST | `/uploads/quote` | Estimate upload cost |
| GET | `/uploads/{id}/download` | Download completed file |
| PUT | `/uploads/{id}/tags` | Set tags on upload |
| GET | `/tags/keys` | List distinct tag keys |
| GET | `/tags/values?key=X` | List distinct values for a key |
| GET | `/tags/search` | Search by selector or tag.key=value |
| POST | `/tags/bulk` | Bulk apply/remove tags |
| GET | `/tags/facets` | Aggregated tag counts |
| POST | `/collections` | Create collection |
| GET | `/collections` | List collections |
| GET | `/collections/{id}` | Get collection with files |
| PUT | `/collections/{id}` | Update collection |
| DELETE | `/collections/{id}` | Delete collection (cascades) |
| POST | `/collections/{id}/files` | Add file to collection (inherits collection tags) |
| DELETE | `/collections/{id}/files/{uploadId}` | Remove file |
| GET | `/collections/{id}/tags` | Get collection tags |
| PUT | `/collections/{id}/tags` | Set collection tags |
| GET | `/system/queue-status` | Upload queue depth and throughput |
| POST | `/tokens` | Create API token |
| GET | `/tokens` | List tokens |
| DELETE | `/tokens/{id}` | Revoke token |
| GET | `/notifications/preferences` | Get notification prefs |
| PUT | `/notifications/preferences` | Update notification prefs |

### Admin Endpoints (admin permission required)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/tag-rules` | List auto-tag rules |
| POST | `/admin/tag-rules` | Create auto-tag rule |
| PUT | `/admin/tag-rules/{id}` | Update auto-tag rule |
| DELETE | `/admin/tag-rules/{id}` | Delete auto-tag rule |
| GET | `/admin/users` | List all users |
| GET | `/admin/users/{id}` | Get user |
| PUT | `/admin/users/{id}` | Update user |
| DELETE | `/admin/users/{id}` | Delete user |
| POST | `/admin/users/service-accounts` | Create service account |
| PUT | `/admin/users/{id}/permissions` | Set user permissions |
| GET | `/admin/groups` | List groups |
| POST | `/admin/groups` | Create group |
| PUT | `/admin/groups/{id}` | Update group |
| DELETE | `/admin/groups/{id}` | Delete group |
| POST | `/admin/groups/{id}/members` | Add member |
| DELETE | `/admin/groups/{id}/members/{userId}` | Remove member |
| GET | `/admin/tokens` | List all tokens |
| DELETE | `/admin/tokens/bulk` | Bulk revoke tokens |
| GET | `/admin/wallets` | List wallets |
| POST | `/admin/wallets` | Create wallet |
| PUT | `/admin/wallets/{id}/default` | Set default wallet |
| GET | `/admin/settings` | Get all settings |
| PATCH | `/admin/settings` | Update settings |
| GET | `/admin/settings/export` | Export settings JSON |
| POST | `/admin/settings/import` | Import settings JSON |
| GET | `/admin/webhooks` | List webhooks |
| POST | `/admin/webhooks` | Create webhook |
| PUT/PATCH | `/admin/webhooks/{id}` | Update webhook |
| DELETE | `/admin/webhooks/{id}` | Delete webhook |
| POST | `/admin/webhooks/{id}/test` | Send test ping |
| GET | `/admin/webhooks/{id}/deliveries` | Delivery history |
| GET | `/admin/webhooks/dead-letters` | Failed-delivery (dead-letter) queue |
| POST | `/admin/webhooks/dead-letters/{id}/resend` | Re-drive a failed delivery |
| DELETE | `/admin/webhooks/dead-letters/{id}` | Dismiss a dead-letter entry |
| GET | `/admin/oidc/providers` | List OIDC providers |
| POST | `/admin/oidc/providers` | Create provider |
| PUT | `/admin/oidc/providers/{id}` | Update provider |
| DELETE | `/admin/oidc/providers/{id}` | Delete provider |
| GET | `/admin/analytics/uploads` | Upload analytics |
| GET | `/admin/analytics/tokens` | Token usage analytics |
| GET | `/admin/analytics/costs` | Cost analytics |
| GET | `/admin/logs/audit` | Query audit logs |
| GET | `/admin/logs/system` | Query system logs |
| GET | `/admin/logs/user` | Query user activity |
| GET | `/admin/quotas` | List quotas |
| POST | `/admin/quotas` | Create quota |
| PUT | `/admin/quotas/{id}` | Update quota |
| DELETE | `/admin/quotas/{id}` | Delete quota |

### Notification Preferences

Users can configure per-user notification settings:

```bash
curl -X PUT https://files.acme.com/api/v2/notifications/preferences \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "webhook_url": "https://hooks.slack.com/...",
    "events": "[\"completed\",\"failed\"]",
    "digest_mode": "daily"
  }'
```

Digest modes: `realtime` (default), `daily`, `weekly`.
