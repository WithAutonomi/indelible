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
13. [Admin: Analytics](#admin-analytics)
14. [Admin: Logs](#admin-logs)
15. [Maintenance Mode](#maintenance-mode)
16. [Rate Limiting](#rate-limiting)
17. [Disk Space Monitoring](#disk-space-monitoring)
18. [Deployment](#deployment)
19. [API Reference](#api-reference)

---

## Getting Started

### Quick Start

```bash
# Set required JWT secret
export INDELIBLE_JWT_SECRET="your-secret-key-at-least-32-chars"

# Run with defaults (SQLite, port 8080)
./indelible --config indelible.toml
```

Open `http://localhost:8080` in your browser. **The first user to register automatically becomes admin.**

### Requirements

- **antd daemon** — running and accessible (default: `http://localhost:8081`)
- **Database** — SQLite (default, zero-config) or PostgreSQL 14+

---

## Configuration

Indelible reads configuration from a TOML file and/or environment variables. Environment variables take precedence.

### Config File (`indelible.toml`)

```toml
port = 8080
db_url = "sqlite:///var/lib/indelible/data.db"
antd_url = "http://localhost:8081"
data_dir = "/var/lib/indelible"
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
| `INDELIBLE_ANTD_URL` | antd daemon URL | `http://localhost:8081` |
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

1. Navigate to `/register` in your browser
2. Enter email, password (minimum 8 characters), first name, last name
3. The first registered user automatically receives admin permissions

### Login

Navigate to `/login` and enter your email and password. Login is rate-limited to **5 attempts per minute per IP** to prevent brute force attacks.

### Password Reset

1. Click "Forgot password?" on the login page
2. Enter your email address — a reset link is sent if SMTP is configured
3. Click the link in your email and set a new password
4. The response is always the same regardless of whether the email exists (prevents email enumeration)

**Note:** Password reset requires SMTP configuration. Without it, reset tokens are logged to server output.

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

### Cost Estimation

Get a cost estimate before uploading:

```bash
# By file size (rough estimate)
curl -X POST https://files.acme.com/api/v2/uploads/quote \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"file_size": 1048576, "visibility": "private"}'

# By actual file (exact cost)
curl -X POST https://files.acme.com/api/v2/uploads/quote \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@document.pdf"
```

---

## Collections & Tags

### Tags

Add metadata to uploaded files using key-value tags:

```bash
curl -X PUT https://files.acme.com/api/v2/uploads/{uuid}/tags \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"department": "legal", "project": "alpha", "client": "acme"}'
```

Tags are **replace-all** — each PUT replaces all existing tags on the file.

### Searching by Tags

```bash
# Search by tag
curl "https://files.acme.com/api/v2/tags/search?tag.department=legal" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Search by filename
curl "https://files.acme.com/api/v2/tags/search?q=contract" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Combined
curl "https://files.acme.com/api/v2/tags/search?tag.department=legal&q=contract" \
  -H "Authorization: Bearer YOUR_TOKEN"
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

### Export / Import

Settings can be exported as JSON for backup or replication to another instance:

1. Go to **Admin > Settings**
2. Click **Export** to download `indelible-settings.json`
3. On another instance, click **Import** and select the file

All settings changes are recorded in the config audit trail with old/new values, who changed them, and the source IP.

---

## Admin: Webhooks

*Requires admin permissions.*

Webhooks send HTTP notifications when upload status changes (queued, processing, completed, failed).

### Setup

1. Go to **Admin > Settings** (Webhooks section)
2. Enter your webhook URL and click **Add**

### Integration Types

- **Generic** — raw JSON payload with event type, timestamp, and upload details
- **Slack** — formatted Block Kit message suitable for Slack incoming webhooks

### Delivery

- Async (non-blocking to upload flow)
- 3 retry attempts with exponential backoff
- 5-second timeout per attempt

### Payload Format (Generic)

```json
{
  "event_type": "completed",
  "timestamp": "2026-03-13T12:00:00Z",
  "upload": {
    "uuid": "abc-123-...",
    "filename": "document.pdf",
    "status": "completed",
    "file_size": 1048576,
    "visibility": "private",
    "actual_cost": "5000000"
  }
}
```

---

## Admin: OIDC / SSO Providers

*Requires admin permissions.*

Indelible supports OpenID Connect for single sign-on with identity providers like Google, Microsoft, Keycloak, etc.

### Adding a Provider

1. Go to **Admin > Settings** (OIDC section)
2. Create a provider with:
   - **Name** — internal identifier (e.g., `google`)
   - **Display Name** — shown on login page (e.g., `Sign in with Google`)
   - **Issuer URL** — OIDC issuer (e.g., `https://accounts.google.com`)
   - **Client ID** and **Client Secret** — from your identity provider
   - **Scopes** — default: `openid email profile`

Client secrets are encrypted at rest using AES-256-GCM.

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

Three log tiers:

### Audit Logs

Security events: logins, permission changes, config changes, token operations. **Never deleted** (compliance). Filterable by event type, user, and date range.

### System Logs

Internal operations: worker events, webhook delivery, errors. Searchable by level (info/warn/error) and component. Subject to log retention settings.

### User Activity Logs

User actions: uploads, downloads, tag changes. Filterable by user and date range. Subject to log retention settings.

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

The login endpoint is rate-limited to **5 attempts per 60 seconds per IP address**. When exceeded, requests receive a `429 Too Many Requests` response.

This protects against brute-force password attacks. The rate limiter uses the `X-Forwarded-For` header when behind a reverse proxy — ensure `trusted_proxies` is configured.

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

## Deployment

### Minimal (Single Binary + SQLite)

```bash
export INDELIBLE_JWT_SECRET="$(openssl rand -hex 32)"
export INDELIBLE_ANTD_URL="http://localhost:8081"
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

```yaml
services:
  indelible:
    image: autonomi/indelible:latest
    ports:
      - "8080:8080"
    volumes:
      - indelible-data:/data
    environment:
      INDELIBLE_JWT_SECRET: "your-secret-key"
      INDELIBLE_ANTD_URL: http://antd:8081
      INDELIBLE_WALLET_ENCRYPTION_KEY: "your-64-char-hex-key"
      INDELIBLE_CORS_ORIGINS: https://files.acme.com

  antd:
    image: autonomi/antd:latest
    ports:
      - "8081:8081"

volumes:
  indelible-data:
```

### Trusted Proxies

When behind a reverse proxy, configure trusted proxy ranges so Indelible correctly reads client IPs from `X-Forwarded-For`:

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
| GET | `/health` | Health check |

### User Endpoints (authenticated)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/me` | Get profile |
| PUT | `/me` | Update profile |
| PUT | `/me/password` | Change password |
| POST | `/uploads` | Upload file (multipart) |
| GET | `/uploads` | List uploads |
| GET | `/uploads/{id}` | Get upload by UUID |
| POST | `/uploads/quote` | Estimate upload cost |
| GET | `/uploads/{id}/download` | Download completed file |
| PUT | `/uploads/{id}/tags` | Set tags on upload |
| GET | `/tags/search` | Search by tags/filename |
| POST | `/collections` | Create collection |
| GET | `/collections` | List collections |
| GET | `/collections/{id}` | Get collection with files |
| PUT | `/collections/{id}` | Update collection |
| DELETE | `/collections/{id}` | Delete collection (cascades) |
| POST | `/collections/{id}/files` | Add file to collection |
| DELETE | `/collections/{id}/files/{uploadId}` | Remove file |
| POST | `/tokens` | Create API token |
| GET | `/tokens` | List tokens |
| DELETE | `/tokens/{id}` | Revoke token |
| GET | `/notifications/preferences` | Get notification prefs |
| PUT | `/notifications/preferences` | Update notification prefs |

### Admin Endpoints (admin permission required)

| Method | Path | Description |
|--------|------|-------------|
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
| PUT | `/admin/webhooks/{id}` | Update webhook |
| DELETE | `/admin/webhooks/{id}` | Delete webhook |
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
