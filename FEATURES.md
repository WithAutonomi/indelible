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
- Single-org model: one instance per company deployment

### 1.2 Network Layer (Delegated to antd)
All Autonomi network operations go through the antd daemon via `antd-go` SDK:
- **Data** — immutable public/private blob storage
- **Files/Dirs** — file and directory upload/download with archive manifests
- **Pointers** — mutable references (used internally for version tracking)
- **Scratchpads** — versioned mutable state
- **Registers** — 32-byte mutable values
- **Vaults** — private encrypted key-value storage
- **Graph Entries** — append-only DAG nodes (used for audit trails)
- **Chunks** — low-level content-addressed blocks

Cost estimation, wallet payment, and deduplication are handled transparently by antd.

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
- One-time use reset tokens with 1-hour TTL
- Completing reset revokes all existing sessions
- Webhook notification on reset request (configurable)

### 2.3 Email Verification
- Verification tokens with configurable TTL
- One-time use tokens
- Required before certain actions (configurable)

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

---

## 3. User Management

### 3.1 User Accounts
- Fields: email (unique), first/last name, active status, email verified
- Per-user upload restrictions:
  - `max_file_size_bytes` (nullable = use system default)
  - `allowed_file_types` (array of extensions, empty = no restriction)
- Soft delete (preserves upload history, prevents cascade data loss)
- Last login tracking

### 3.2 Permission Model
Three permission levels: **read**, **write**, **admin**

**Assignment methods:**
1. **Direct permissions** — granted by admins per user, tracked with `granted_by`
2. **Group permissions** — inherited from group membership
3. **Effective permissions** = direct ∪ group-inherited

**Hierarchy:** admin implies write implies read

**Safety:**
- System prevents removing the last admin
- Warning on group changes that would orphan users (can force override)

### 3.3 Groups
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
- **Default expiry:** system-configurable (default 90 days)
- **Revocation:** soft-delete (sets revoked flag), logged with reason and who revoked
- **Bulk revocation:** admin can revoke multiple tokens in one request
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
- Global webhook URL + enabled toggle
- Integration type: **Generic** (raw JSON) or **Slack** (Block Kit formatted)
- Event subscription: select which status transitions trigger webhooks
  - Queued, Processing, Completed, Failed

### 8.2 Delivery
- Async fire-and-forget (non-blocking to upload flow)
- 3 retry attempts with exponential backoff
- 5-second timeout per attempt
- Delivery logged at system level

### 8.3 Payloads
**Generic:** JSON with event_type, timestamp, upload details (id, filename, status, size, cost, error)
**Slack:** Formatted blocks with markdown, file info, cost display

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
- **System Settings** — all runtime config from section 10
- **Webhook Settings** — URL, integration type, event subscriptions
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
- **PostgreSQL** (14+)
- **antd daemon** (running and accessible)

### 13.4 Data Directory
- Temp upload storage (configurable path)
- Log files (daily rotation)
- Cleaned up on completion/failure

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
- Access to all 8 data primitives (v1 only used Data for file uploads)
- File-focused initially, with future extension path to vaults and archives
- Cost estimation across all primitive types (not just files)

### Architecture Changes
- Go single binary vs Rust + separate Nginx
- Embedded frontend vs separate Docker service
- antd REST client vs embedded autonomi crate
- Dual database support: PostgreSQL (enterprise) + SQLite (zero-config)
- Single-org model: one instance per company, company manages own DNS/routing

---

## Decisions Made

1. **Database: PostgreSQL + SQLite dual support.** SQLite is the zero-config default (embedded in binary, true single-file deployment). PostgreSQL is recommended for production/scale (10+ concurrent users, 100K+ uploads). Schema differences managed via dialect-specific migrations (~10% of SQL diverges on arrays, JSONB, indexes).

2. **Notifications: Webhook-only (same as v1).** No built-in SMTP. Password reset and upload events fire webhooks — the deploying company hooks into their own email/notification system. Keeps the binary simple and avoids SMTP configuration complexity.

3. **Scope: File-focused.** API exposes file upload/download and cost estimation. Does not expose raw pointers, scratchpads, registers, graph entries to end users. Future extension path to **vaults** (encrypted private storage) and **archives** (browsable file collections).

4. **Tenancy: Single-org.** One indelible instance per company. The company installs it, configures their DNS, manages their own users/groups/tokens. No multi-tenant isolation layer needed.

5. **Default database: SQLite.** SQLite is the default for initial customers (smaller deployments). PostgreSQL available for scale. SQLite file copy serves as full backup.

6. **API versioning: `/api/v2/`** — aligns with Autonomi network versioning.

---

## Remaining Open Questions

1. **Backup/restore scope** — Options: (A) no in-app backup, document "copy .db file", (B) settings-only export/import for config migration between instances, (C) full export like v1. Recommend B.
