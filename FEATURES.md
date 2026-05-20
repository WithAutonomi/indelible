# Indelible v2 — Feature Specification

**Product:** Enterprise gateway for Autonomi decentralized storage
**Stack:** Go + Vue 3 + PostgreSQL + antd-go SDK
**Distribution:** Single binary (Go embed) + Docker option
**Date:** 2026-03-12

---

## 1. Core Architecture

### 1.1 Single Binary Deployment
- Go binary embeds Vue 3 SPA via `go:embed`
- Single process serves API + static frontend + background workers
- Configuration via environment variables and/or config file (TOML/YAML)
- **Database:** SQLite by default (zero-config, embedded), PostgreSQL for production scale
  - SQLite: `--db sqlite:///var/indelible/data.db` (or default path in data dir)
  - PostgreSQL: `--db postgres://user:pass@host/indelible`
- Connects to `antd` daemon over REST for all network operations
  - **Managed mode:** `antd_managed = true` — indelible spawns, monitors, and stops antd as a child process (auto port discovery, crash restart)
  - **External mode:** point `antd_url` at a separately-run antd instance (recommended for production / Docker Compose)
- Single-org model: one instance per company deployment

### 1.2 Network Layer (Delegated to antd)
All Autonomi network operations go through the antd daemon via `antd-go` SDK. Indelible uses **immutable data types only**:
- **Data** — immutable public/private blob storage
- **Files/Dirs** — file and directory upload/download with archive manifests

No mutable network primitives (pointers, scratchpads, registers, graph entries) are used. Cost estimation, wallet payment, and deduplication are handled transparently by antd.

### 1.3 API Consumer Affordances
- **Request ID tracing** — `X-Request-Id` header on every response for log correlation
- **Rate limit headers** — `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` on rate-limited endpoints; `Retry-After` on 429
- **Structured error codes** — machine-readable `code` field in error responses alongside human-readable `error`
- **Idempotency keys** — `Idempotency-Key` header on POST /uploads for safe retries; cached responses replayed for 24h
- **Webhook signatures** — HMAC-SHA256 signing of outbound webhook payloads with `X-Signature-256` header; per-webhook secrets with rotation support
- **Cursor pagination** — keyset-based pagination via `cursor` query param as alternative to offset/limit
- **Upload filtering** — `status`, `sort`, `from`, `to` query params on upload listing

---

## 2. Authentication & Identity

### 2.1 Local Authentication
- Email + password registration and login
- Password requirements: minimum 8 characters
- JWT-based sessions with configurable expiry (default 24h)
- Session tokens hashed before storage (bcrypt)
- Password change revokes all other sessions (force re-login)
- First registered user automatically becomes admin

### 2.2 Password Reset
- Request reset via email address
- Constant-time response regardless of email existence (prevents enumeration)
- One-time use reset tokens (32 random bytes, hex-encoded) with 1-hour TTL
- Reset link sent via SMTP (configurable, falls back to log warning if SMTP not configured)
- Completing reset revokes all existing sessions
- Service accounts and inactive users cannot request resets

### 2.3 Email Verification
- Verification email sent on registration (best-effort, non-blocking)
- 24-hour one-time-use tokens stored in `email_verification_tokens` table
- `GET /auth/verify-email?token=...` validates and sets `email_verified` flag
- `POST /me/resend-verification` for authenticated users to request a new email
- Uses the same `Notifier` interface as password reset (SMTP or noop)

### 2.4 OIDC / SSO
- Global enable/disable switch
- Multiple concurrent providers (Google, Microsoft, Keycloak, any OIDC-compliant)
- Per-provider configuration:
  - Display name (shown on login page)
  - Issuer URL, client ID, client secret
  - Custom scopes (default: openid, email, profile)
  - Per-provider enable/disable toggle
- PKCE (SHA256) for all OIDC flows
- State + nonce validation with 10-minute TTL
- **Security:** No automatic email-based account linking (prevents email confusion attacks)
- Auto-provisioning toggle: create user on first SSO login with configurable default permissions
- Manual identity linking: authenticated users can link additional providers
- Identity unlinking with safety check (cannot unlink last login method if no password)

### 2.5 SCIM 2.0 Provisioning
- Industry-standard protocol for automated user/group provisioning from identity providers
- Supported IdPs: Okta, Azure AD/Entra, Google Workspace, any SCIM 2.0 client
- **Endpoints:** `/scim/v2/Users`, `/scim/v2/Groups`, `/scim/v2/ServiceProviderConfig`, `/scim/v2/Schemas`, `/scim/v2/ResourceTypes`
- Full CRUD: Create, Read (single + list), Replace (PUT), Patch, Delete
- Filter support: `userName eq "..."` optimized to direct DB lookup
- SCIM-provisioned users have no password (authenticate via OIDC/SSO)
- SCIM-provisioned groups default to "read" permission level
- `externalId` mapping stored on users and groups for IdP correlation
- Separate SCIM bearer tokens (prefixed `scim_`, bcrypt-hashed) managed via admin API
- Global enable/disable toggle via admin settings
- Audit logging for all SCIM operations
- Admin UI: enable/disable toggle, token generation with one-time secret display, token management table

---

## 3. User Management

### 3.1 User Accounts
- Fields: email (unique), first/last name, active status, email verified
- Per-user upload restrictions:
  - `max_file_size_bytes` (nullable = use system default)
  - `allowed_file_types` (array of extensions, empty = no restriction)
- Soft delete (preserves upload history, prevents cascade data loss)
- Last login tracking

### 3.2 Service Accounts
Non-human accounts for CI pipelines, integrations, and shared team use.

- Flagged as `is_service_account = true` on the users table
- No password, no login, no SSO — exist only to own API tokens
- Created and managed by admins via dashboard or API
- Have their own permissions (direct or group-inherited), same model as regular users
- Have their own upload restrictions (file size, file types)
- Tokens owned by a service account survive employee departures
- Visible in admin user management with clear service account indicator
- Soft delete supported (revokes all owned tokens on deactivation)

### 3.3 Permission Model
Three permission levels: **read**, **write**, **admin**

**Assignment methods:**
1. **Direct permissions** — granted by admins per user, tracked with `granted_by`
2. **Group permissions** — inherited from group membership
3. **Effective permissions** = direct ∪ group-inherited

**Hierarchy:** admin implies write implies read

**Safety:**
- System prevents removing the last admin
- Warning on group changes that would orphan users (can force override)

### 3.4 Groups
- Named groups with description
- Each group grants a single permission level (read, write, or admin)
- Active/inactive toggle
- Membership tracking with `added_by` audit trail
- Cascade: deactivating/deleting group affects member permissions

---

## 4. API Token Management

### 4.1 Token Properties
- UUID identifier (used in API responses)
- Opaque secret shown once at creation (never stored, only hash retained)
- Name, description (optional, HTML-sanitized)
- Permission set: array of (read/write/admin)
- Client department (optional, for cost allocation/analytics)
- Expiry: configurable per-token, nullable = no expiry, capped at 10 years
- Per-token upload restrictions:
  - `max_file_size_bytes`
  - `allowed_file_types`

### 4.2 Token Lifecycle
- **Creation:** regular users get read/write tokens only; admins can create admin tokens
- **Ownership:** tokens owned by the creating user or by a service account
- **Service account tokens:** created by admins against a service account, survive employee departures
- **Default expiry:** system-configurable (default 90 days)
- **Revocation:** soft-delete (sets revoked flag), logged with reason and who revoked
- **Bulk revocation:** admin can revoke multiple tokens in one request
- **Auto-revocation:** all tokens revoked when owning user/service account is deactivated
- **Cannot unrevoke:** create a new token instead
- **Retained in DB:** for audit trail even after revocation

### 4.3 Token Usage Tracking
- `usage_count` incremented on each API request
- `last_used_at` timestamp updated
- Detailed usage log per request: endpoint, HTTP method, IP, user-agent, response status
- Revocation audit: separate table tracking who revoked, when, and why

---

## 5. File Operations

### 5.1 Upload Flow (Async Queue)
1. Client posts multipart file to upload endpoint
2. Server validates: file size (system + token/user limits), file type restrictions, wallet availability
3. File written to temp storage, upload record created as **Queued**
4. Background worker picks up task (respects `max_concurrent_uploads` config)
5. Status transitions: **Queued → Processing → Completed / Failed**
6. On completion: datamap stored, actual cost recorded, temp file cleaned
7. On failure: error recorded, temp file cleaned
8. Webhooks fired on each status transition (if subscribed)

### 5.2 Upload Properties
- Visibility: public or private (default private)
- Filename sanitization (path traversal prevention, null byte stripping)
- Cost estimation available before upload (`/quote` endpoint)
- Datamap stored on completion (network location reference)
- Both estimated and actual cost tracked

### 5.3 Queue Management
- Configurable concurrent upload limit (default 1, max 100)
- Crash recovery: on startup, requeue Queued uploads from DB
- Reconciliation worker: periodic check (5 min) for stuck uploads
  - Queued > 2 minutes and not in queue → re-enqueue or mark failed
- Queue status monitoring

### 5.4 Download
- Download by datamap address
- Respects visibility (public data freely accessible, private requires auth)

---

## 6. Wallet Management

### 6.1 Wallet Storage
- Multiple wallets supported
- Exactly one default wallet (used for uploads)
- Private keys encrypted at rest (AES-256-GCM using a server-side secret)
- Balance tracking: payment balance + gas balance
- Balance refresh from network on demand

### 6.2 Transaction History
- Per-wallet transaction log
- Links transactions to specific uploads
- Records: transaction type, amount, balance after

---

## 7. Analytics & Cost Tracking

### 7.1 Upload Analytics
- Total uploads by status (queued, processing, completed, failed)
- Success/failure rates
- Average processing time
- Average upload size
- Total bytes uploaded
- Status distribution over time
- Top uploaders
- Recent failures (for debugging)

### 7.2 Token Usage Analytics
- Total requests across all tokens
- Active token count
- Requests by endpoint breakdown
- Most active tokens with last-used timestamps
- Per-token detailed usage history (endpoint, method, IP, timestamp)

### 7.3 Cost Analytics
- **Per-token:** total cost, upload count, bytes uploaded, average cost per upload/byte
- **Per-department:** aggregated across tokens sharing a department tag
- **System-wide:** overall totals
- **Cache hit tracking:** zero-cost uploads (deduplication) tracked separately, excluded from cost averages
- Time-filtered: all endpoints accept `since` parameter (default 7 days)
- Views optimized via PostgreSQL materialized or standard views

---

## 8. Webhook Notifications

### 8.1 Configuration
- Multiple webhook endpoints, each independently configured
- Per-webhook enable/disable toggle
- Integration type: **Generic** (raw JSON) or **Slack** (Block Kit formatted)
- Per-webhook event subscription: select which events trigger delivery
  - Upload events: Queued, Processing, Completed, Failed
  - System events: Disk Warning (80%), Disk Critical (95%), Disk Recovered
  - Auth events: Password reset requested, Email verification requested (required when Email Notifier is set to Webhook — see §2.2)
  - Integration events: Tags updated, Collection file added/removed
- Dedicated admin page (**Admin > Webhooks**) with inline edit, event tag display
- Test ping button per webhook for verifying endpoint connectivity

### 8.2 Event Types

| Event | Category | Source | Description |
|-------|----------|--------|-------------|
| `queued` | Upload | Upload handler | File accepted and queued for processing |
| `processing` | Upload | Upload worker | Worker started processing the upload |
| `completed` | Upload | Upload worker | Upload successfully stored on network |
| `failed` | Upload | Upload worker | Upload processing failed |
| `disk_warning` | System | Disk alert worker | Disk usage reached warning threshold (80%) |
| `disk_critical` | System | Disk alert worker | Disk usage reached critical threshold (95%), uploads paused |
| `disk_recovered` | System | Disk alert worker | Disk usage returned below warning threshold |
| `auth.password_reset_requested` | Auth | Password-reset flow | Reset link generated; payload carries recipient email + reset URL |
| `auth.email_verification_requested` | Auth | Email-verification flow | Verification link generated; payload carries recipient email + verify URL |
| `tags_updated` | Integration | Tag handlers | Tags on an upload were changed |
| `collection_file_added` | Integration | Collection handler | Upload added to a collection |
| `collection_file_removed` | Integration | Collection handler | Upload removed from a collection |
| `test_ping` | Test | Admin API | Manual test ping from admin UI |

### 8.3 Delivery
- Async fire-and-forget (non-blocking to upload flow)
- Event filtering: only delivers events the webhook is subscribed to
- 3 retry attempts with exponential backoff (2s, 4s between retries)
- 5-second timeout per attempt
- Delivery log: every attempt recorded in `webhook_delivery_log` table with status code, success, attempts, error
- Delivery log auto-pruned alongside log retention settings
- System alert deduplication: disk alerts only fire once per threshold transition (not every check interval)

### 8.4 Payloads
**Generic (upload event):** JSON with `event_type`, `timestamp`, `upload` object (uuid, user_id, token_id, filename, status, file_size, visibility, actual_cost, error_message). `token_id` included when the upload was created via API token, enabling consumers to filter for their own uploads.
**Generic (system event):** JSON with `event_type`, `timestamp`, `system` object (alert_type, message, value)
**Generic (auth event):** JSON with `event_type`, `timestamp`, `auth` object (`to`, `url`). Receivers are expected to deliver `url` to `to` via their preferred channel (transactional email API, Slack DM, etc.).
**Generic (tag event):** JSON with `event_type`, `timestamp`, `tags` object (upload_uuid, tags: map<string, string[]>)
**Generic (collection event):** JSON with `event_type`, `timestamp`, `collection` object (upload_uuid, collection_id, collection_name)
**Slack:** Formatted Block Kit messages with markdown — upload events show file info and cost; system events show alert type and percentage; auth events show recipient address and a clickable link; tag/collection events show the affected upload UUID

### 8.5 Admin API
- `POST /api/v2/admin/webhooks/{id}/test` — synchronous test ping, returns HTTP status code and success/failure
- `GET /api/v2/admin/webhooks/{id}/deliveries` — recent delivery history with status, attempts, errors

---

## 9. Logging & Audit

### 9.1 Three-Tier Logging
| Type | Purpose | Retention |
|------|---------|-----------|
| **Audit** | Security events, permission changes, config changes | Never deleted (compliance) |
| **System** | Internal operations (workers, webhooks, errors) | Configurable retention (default 30 days) |
| **User** | User activity (uploads, token creation, logins) | Configurable retention |

### 9.2 Features
- Daily rotating log files
- Searchable and filterable (by date, user, event type, severity)
- Downloadable by date (JSON Lines format)
- Statistics per log type (entry count, disk usage, date range, breakdown by level/event)
- Config change audit trail: records old value, new value, who changed, when, from where (IP + user-agent)

### 9.3 Log Retention
- Enable/disable toggle
- Configurable retention period (1–365 days)
- Audit logs exempt from retention (permanent)

---

## 10. System Configuration (Runtime)

All settings stored in DB, changeable at runtime without restart.

| Setting | Default | Constraints |
|---------|---------|------------|
| `maintenance_mode` | false | — |
| `maintenance_message` | null | 0–1000 chars |
| `max_upload_size_bytes` | 10 GB | 1 MB – 100 GB |
| `jwt_expiry_hours` | 24 | 1–8760 |
| `default_token_expiry_days` | 90 | 1–3650 |
| `max_concurrent_uploads` | 1 | 1–100 |
| `environment_name` | "production" | free text |
| `cors_allowed_origins` | localhost:5173 | valid URLs |
| `timezone` | UTC | IANA timezone |
| `rate_limit_login_attempts` | 5 | 1–100 |
| `rate_limit_login_window_secs` | 60 | 1–3600 |
| `log_retention_enabled` | false | — |
| `log_retention_days` | 30 | 1–365 |
| `default_user_permissions` | [read] | subset of read/write/admin |

**Update behavior:**
- Partial updates supported (only specified fields change)
- All changes logged to config audit trail with old/new values

---

## 11. Admin Dashboard (Web UI)

### 11.1 Auth Pages
- Login (email/password + SSO provider buttons)
- Registration
- Forgot password / Reset password
- OIDC callback handler

### 11.2 User Pages
- **Dashboard** — drag-and-drop file upload, upload status monitor
- **Uploads** — list/filter uploads, view status, download completed files
- **Profile** — edit name, change password, manage linked SSO identities
- **API Tokens** — create/revoke personal tokens, view usage

### 11.3 Admin Pages
- **User Management** — list users, edit permissions, activate/deactivate, soft-delete
- **Group Management** — create/edit groups, manage membership
- **Wallet Management** — create wallets, set default, view balances, transaction history
- **System Settings** — all runtime config from section 10, per-card save/discard
- **Webhooks** — dedicated page: create/edit/delete endpoints, integration type, event subscriptions, test ping, delivery history
- **OIDC Settings** — provider management, auto-provision toggle
- **Analytics: Tokens** — usage charts, active token stats
- **Analytics: Uploads** — upload volume, success rates, processing times
- **Analytics: Costs** — per-token, per-department, system-wide cost breakdowns
- **Logs** — tabbed view (audit/system/user), search, filter, download

---

## 12. API Design

### 12.1 Authentication
- Bearer token in Authorization header (JWT for sessions, opaque for API tokens)
- Both token types checked against revocation
- Rate limiting on login endpoint

### 12.2 Middleware
- Maintenance mode check (returns 503 with message when active)
- CORS with configurable origins
- Rate limiting (configurable per route)
- Request logging for API tokens

### 12.3 API Documentation
- OpenAPI/Swagger auto-generated and served from the binary
- Versioned API paths (`/api/v2/...`)

---

## 13. Deployment & Operations

### 13.1 Single Binary
- `indelible` binary serves everything (API + embedded SPA + workers)
- Configuration via env vars or `indelible.toml`
- Flags: `--port`, `--db-url`, `--antd-url`, `--data-dir`
- Health endpoint for load balancer/monitoring integration

### 13.2 Docker
- Single-container option (binary + external PostgreSQL)
- Docker Compose with PostgreSQL included
- Published to container registry

### 13.3 Required External Services
- **antd daemon** (running and accessible)
- **PostgreSQL** (14+) — only if opting out of default SQLite

### 13.4 Data Directory
- Temp upload storage (configurable path)
- Log files (daily rotation)
- SQLite database file (when using default DB)
- Cleaned up on completion/failure

### 13.5 Domain Setup & Reverse Proxy

Indelible does not handle TLS/SSL directly. Companies place a reverse proxy in front of it — the standard pattern for all self-hosted enterprise applications (GitLab, Grafana, Mattermost, etc.).

**Typical network path:**
```
Users → https://files.acme.com → Reverse Proxy (TLS) → localhost:8080 (Indelible)
```

**Company steps:**
1. Create DNS A record pointing their chosen domain (e.g. `files.acme.com`) to the server IP
2. Configure a reverse proxy with TLS termination
3. Set `cors_allowed_origins` in Indelible to match the domain

**Supported reverse proxies (documented with examples):**

**Caddy** (simplest — automatic Let's Encrypt TLS):
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
        proxy_set_header X-Forwarded-For $proxy_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**Corporate load balancers:** F5, AWS ALB, Azure App Gateway, Kubernetes ingress — Indelible is added as a backend target like any other internal service.

**Indelible requirements for proxy support:**
- Respects `X-Forwarded-For` and `X-Forwarded-Proto` headers for correct IP logging and HTTPS detection
- `trusted_proxies` config option (CIDR ranges or exact IPs) — `X-Forwarded-For` is only honoured when the direct connection comes from a trusted proxy. Prevents header spoofing for rate limiting and audit logging.
- Shared `middleware.ClientIP()` helper used by rate limiter and all audit logging
- Health endpoint (`/health`) for load balancer health checks

### 13.6 Typical Deployment Walkthrough

**Minimal (single binary, SQLite, smallest setup):**
```bash
# 1. Download
curl -LO https://releases.autonomi.com/indelible/latest/indelible-linux-amd64
chmod +x indelible-linux-amd64

# 2. Run (SQLite default, port 8080)
./indelible-linux-amd64 --antd-url http://localhost:8082

# 3. Open browser, register first user (auto-admin), configure from dashboard
```

**Production (behind Caddy, SQLite or PostgreSQL):**
```bash
# 1. Install indelible binary + Caddy + antd daemon on server

# 2. Configure indelible
cat > indelible.toml <<EOF
port = 8080
antd_url = "http://localhost:8082"
data_dir = "/var/lib/indelible"
cors_allowed_origins = ["https://files.acme.com"]
trusted_proxies = ["127.0.0.1/32"]
EOF

# 3. Configure Caddy
echo 'files.acme.com { reverse_proxy localhost:8080 }' > /etc/caddy/Caddyfile

# 4. Create DNS A record: files.acme.com → server IP

# 5. Start services
systemctl start antd
systemctl start indelible
systemctl start caddy

# 6. Open https://files.acme.com, register admin, configure SSO/wallets/permissions
```

**Docker Compose:**
```yaml
services:
  indelible:
    image: autonomi/indelible:latest
    ports:
      - "8080:8080"
    volumes:
      - indelible-data:/data
    environment:
      INDELIBLE_ANTD_URL: http://antd:8082
      INDELIBLE_CORS_ORIGINS: https://files.acme.com
      INDELIBLE_TRUSTED_PROXIES: 172.16.0.0/12

  antd:
    image: autonomi/antd:latest
    ports:
      - "8082:8082"

volumes:
  indelible-data:
```

---

## 14. Security

- Passwords: bcrypt hashed, minimum 8 characters
- JWT: signed with configurable secret, configurable expiry
- Session tokens: bcrypt hashed before DB storage
- API tokens: bcrypt hashed, secret shown once
- Wallet keys: AES-256-GCM encrypted at rest
- OIDC: PKCE + state + nonce validation
- Input sanitization: HTML stripped from user inputs
- Filename sanitization: path traversal and null byte prevention
- No internal paths exposed in error responses
- Constant-time responses for email existence checks
- Session invalidation on password change
- Soft delete to prevent data loss

---

## v2 Changes from v1

### Removed / Simplified
- No direct Autonomi crate dependency (delegated to antd)
- No Autonomi Bridge trait (antd abstracts this)
- No compile-time SQL (use Go DB abstraction supporting both PostgreSQL and SQLite)

### New Capabilities Enabled by ant-sdk
- Clean separation from network layer via antd daemon
- Immutable data and file operations only (no mutable primitives)
- Future extension path to vaults and archives

### Architecture Changes
- Go single binary vs Rust + separate Nginx
- Embedded frontend vs separate Docker service
- antd REST client vs embedded autonomi crate
- Dual database support: PostgreSQL (enterprise) + SQLite (zero-config)
- Single-org model: one instance per company, company manages own DNS/routing

---

## Decisions Made

1. **Database: PostgreSQL + SQLite dual support.** SQLite is the zero-config default (embedded in binary, true single-file deployment). PostgreSQL is recommended for production/scale (10+ concurrent users, 100K+ uploads). Schema differences managed via dialect-specific migrations (~10% of SQL diverges on arrays, JSONB, indexes).

2. **Transactional email: Built-in SMTP.** Lightweight SMTP support (Go stdlib `net/smtp`) for transactional emails only: password reset and email verification. Configured via `[smtp]` block in TOML or `INDELIBLE_SMTP_*` env vars. Upload events and system notifications still use webhooks. Future: webhook-based notification delivery as alternative to SMTP for companies that prefer custom routing (see fast-follow FC-16).

3. **Scope: Immutable files only.** API exposes file upload/download and cost estimation using immutable data types only. No mutable network primitives (pointers, scratchpads, registers, graph entries) are used. Future extension path to **vaults** (encrypted private storage) and **archives** (browsable file collections).

4. **Tenancy: Single-org.** One indelible instance per company. The company installs it, configures their DNS, manages their own users/groups/tokens. No multi-tenant isolation layer needed.

5. **Default database: SQLite.** SQLite is the default for initial customers (smaller deployments). PostgreSQL available for scale. SQLite file copy serves as full backup.

6. **API versioning: `/api/v2/`** — aligns with Autonomi network versioning.

7. **Backup/restore: Configuration export/import.** Structured JSON export includes system settings, webhook configurations, OIDC provider configs (without client secrets), and group definitions. Import supports both the structured format and legacy flat settings format for backwards compatibility. OIDC client secrets must be re-entered after import. Full database backup (SQLite file copy or pg_dump) is the deploying company's responsibility, documented in ops guide.

---

## Additional v2 Scope (promoted from consideration)

The following features were evaluated against enterprise archival standards and promoted into the v2 build.

### Metadata, Search & Organisation (FC-1 + FC-7)
File metadata and virtual folder structure, stored in the local database (not on the network).

**Tagging:**
- Custom key-value tags on uploaded files (e.g., `department:legal`, `project:alpha`, `client:acme`)
- Tags set at upload time via `tags` form field (JSON object) or editable after upload via `PUT /uploads/{id}/tags`
- Stored in `file_tags` table with composite indexes for fast lookup
- Auto-tag rules: admin-defined rules that automatically apply tags based on content type, filename regex, file size, or visibility (Admin > Tag Rules)
- Tag facets endpoint (`GET /tags/facets`) returns aggregated key/value/count for filter UIs
- Bulk tag operations (`POST /tags/bulk`) to apply/remove tags across multiple uploads by UUID list or label selector

**Search:**
- Label selector syntax (modeled on Kubernetes): `selector=department=engineering,status!=archived,env in (prod,staging)`
- Operators: `=` (equality), `!=` (inequality), `in` (set membership), `notin` (set exclusion), key exists, `!key` (not exists)
- Backward-compatible `tag.key=value` query params for simple AND-only searches
- Filename substring search via `q` param, combinable with selectors
- All queries use parameterized SQL — no injection risk

**Virtual Folders / Collections:**
- Logical grouping of files into folders or collections (database-only, not on network)
- Hierarchical folder structure (parent/child) for organising uploads
- Collection-level tags (`GET/PUT /collections/{id}/tags`) — inherited by files when added to the collection
- Tag inheritance is additive: collection tags propagate to files without overwriting existing file tags
- Supports bulk operations: bulk tag, bulk legal hold
- Required for dashboard usability at scale — "Case #12345", "Q1 Tax Docs"
- `collections` table with `collection_files` join table

### Storage Quotas (FC-6)
Configurable, default off. Prevents runaway spend.

- Quotas per user, per group, per department, and system-wide
- Expressed in bytes (total uploaded data)
- Checked before accepting new uploads — rejected if quota would be exceeded
- Cumulative tracking from upload records (completed uploads only)
- Admin dashboard shows quota usage per entity
- Configurable: disabled by default, can be enabled and set per entity

### Server Disk Space Alerting (FC-11)
Monitors temp upload directory and data directory for disk fullness.

- Configurable warning threshold (default 80%) and critical threshold (default 95%)
- Periodic check (configurable interval, default 5 minutes)
- Warning threshold: fires webhook notification
- Critical threshold: fires webhook + optionally pauses accepting new uploads
- Paused state visible in dashboard and health endpoint
- Auto-resumes when disk usage drops below warning threshold
- Essential because the upload queue writes files to local disk before network upload — disk-full causes silent failures

### Per-User Notification Preferences (FC-15)
Configurable per-user event subscriptions. Essential for larger deployments where system-wide webhooks create noise.

- Each user configures which events they receive: upload completed, upload failed, permission changes, system alerts
- Delivery via per-user webhook URL (company connects to their notification system)
- Digest mode: aggregate events into daily/weekly summary instead of real-time
- Admin can set organisation-wide defaults; users can override
- Integrates with existing webhook infrastructure

---

## Features — Fast Follow (post-launch updates)

**FC-3: File Integrity Verification & Chain of Custody**
Indelible's key differentiator — cryptographic proof of immutability. Autonomi is content-addressed: the upload address IS a hash of the content. Includes: verification endpoint (re-download and confirm hash), chain-of-custody reports (full upload and access history from audit logs), and signed upload receipts. Low effort — the hard crypto is done by the network; we surface it.

**FC-4: Legal Hold**
Freeze file metadata, tags, and audit records from modification or cleanup during litigation. Files on the network are already permanent; legal hold protects the local metadata layer. Admin creates holds scoped by files, users, tags, or date ranges. Held records cannot be deleted, modified, or re-tagged until released. Implementation: `legal_holds` + `legal_hold_files` tables, cleanup processes check holds before acting.

**FC-2: Multi-Factor Authentication (TOTP)**
Authenticator app as second login factor. Enforceable by policy (all users, admin-only, optional). Baseline for SOC 2/ISO 27001. TOTP first, WebAuthn/FIDO2 later. Lower priority for launch because smaller first customers will primarily use SSO.

**FC-8: IP Allowlisting**
Restrict API/dashboard access to specific IP ranges or CIDR blocks. Global and per-API-token. Common security questionnaire item.

**FC-16: Webhook-Based Notification Delivery**
Alternative to SMTP for transactional emails (password reset, email verification). Companies that route notifications through their own systems (Slack, PagerDuty, custom HTTP endpoints) can configure a webhook URL instead of SMTP. The Notifier interface is already stubbed (`WebhookNotifier` in `internal/services/email.go`) — implementation needs webhook URL config, payload format, and retry logic.

---

## Features — Backlog (customer-driven)

**FC-5: Shared Links with Controls**
Password-protected, expiring, download-limited shareable links for external parties. Backlogged because downloading from Autonomi requires a client — the sharing mechanism needs design work around how external recipients actually retrieve files.

**FC-9: Virus / Malware Scanning (Pluggable)**
ClamAV or webhook-based file scanning before network upload. Optional. Important because network storage is permanent, but adds deployment complexity.

**FC-10: Scheduled Compliance Reports**
PDF/CSV report templates delivered on schedule via webhooks. Data already exists in analytics tables.

**FC-12: SCIM 2.0 Provisioning** — *Implemented (see Section 2.5)*

**FC-13: Content Classification Labels**
Sensitivity levels (Public/Internal/Confidential/Restricted) with policy enforcement. Depends on tagging (FC-1) being mature.

**FC-17: Paymaster / ANT-Only Payments (Smart Wallets)**
Eliminate the requirement for enterprise deployments to hold both ANT and ETH by using ERC-4337 Account Abstraction with the Autonomi Paymaster contract. Users pay for both storage and gas using only ANT — the paymaster sponsors ETH gas and takes the equivalent ANT (priced via Chainlink + Uniswap V3 TWAP oracle). **Blocked on ant-sdk support:** The current `evmlib` wallet uses direct EOA transactions. Paymaster requires constructing UserOperations and routing through a bundler (Pimlico). The `external_signer` module in `autonomi_client` provides the calldata hooks needed — Indelible could get quotes from antd, submit payments as UserOperations via Pimlico's bundler API, and pass the receipt back to antd for upload. Implementation path: add `PaymentOption::SmartAccount` to ant-sdk, or bypass antd for the payment step and interact with the bundler directly from Go. Reference: `WithAutonomi/autonomi-paymaster` (Solidity contract on Arbitrum One at `0x95bf...1EF4`), `WithAutonomi/project-dave` (TypeScript integration using `permissionless.js` + Pimlico).

---

## Dropped Features

**FC-14: Retention Policies with Minimum Hold**
Dropped. Autonomi is permanent storage — data cannot expire or be deleted from the network. The only local data subject to retention is logs, which already have configurable retention. Legal hold (FC-4) covers the compliance case for preserving local metadata. A separate retention policy mechanism solves a problem that doesn't exist on a perpetual storage network.
