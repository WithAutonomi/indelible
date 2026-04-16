# Indelible

Enterprise storage gateway for the [Autonomi](https://autonomi.com) decentralized network. Upload, manage, and retrieve files on permanent decentralized storage with a familiar REST API, admin dashboard, and enterprise identity integrations.

## What it does

Indelible sits between your applications and the Autonomi network. It handles authentication, file management, cost estimation, wallet payments, and audit logging — so you don't have to interact with the network directly.

- **Single binary** — Go backend + Vue 3 frontend, embedded. One process, one database. Can manage the antd daemon automatically or connect to an external instance.
- **SQLite or PostgreSQL** — SQLite by default for zero-config local use; PostgreSQL for production scale.
- **REST API** — full CRUD for uploads, collections, tags, tokens, users, groups, and admin operations. Swagger docs at `/api/docs/`.
- **Admin dashboard** — user management, wallet configuration, quotas, webhooks, OIDC providers, SCIM provisioning, analytics, and audit logs.
- **Enterprise identity** — OIDC/SSO with any provider (Okta, Azure AD, Google), SCIM 2.0 for automated user/group provisioning.

## Quick start

**Prerequisites:** [antd](https://github.com/WithAutonomi/ant-sdk) daemon installed (handles all Autonomi network operations).

```bash
# Build
go build -o indelible ./cmd/indelible

# Run with managed antd (starts antd automatically)
export INDELIBLE_JWT_SECRET="$(openssl rand -hex 32)"
export INDELIBLE_ANTD_MANAGED=true
./indelible
```

Open http://localhost:8080 — the first user to register becomes admin.

### Managed antd (recommended for development)

Indelible can automatically start and manage the antd daemon:

```bash
export INDELIBLE_ANTD_MANAGED=true
./indelible
```

This requires the `antd` binary in your PATH. Indelible will:
- Start antd on a free port
- Discover the port automatically
- Monitor the process and restart on crash (up to 3 times)
- Stop antd when indelible shuts down

### External antd (production)

For production, run antd separately:

```bash
antd &   # binds to 0.0.0.0:8082 by default; override with --rest-port if needed
export INDELIBLE_ANTD_URL=http://localhost:8082
./indelible
```

If antd is already running and no `INDELIBLE_ANTD_URL` is set, indelible will auto-discover it via the `daemon.port` file.

### With a config file

```bash
./indelible --config indelible.toml
```

```toml
port = 8080
db_url = "sqlite://./data.db"
antd_url = "http://localhost:8082"
data_dir = "./data"
jwt_secret = "your-secret-key-at-least-32-chars"

# Optional
base_url = "https://files.yourcompany.com"
cors_allowed_origins = ["https://files.yourcompany.com"]
wallet_encryption_key = "64-hex-char-key-for-aes-256-gcm"
```

### With PostgreSQL

```bash
export INDELIBLE_DB_URL="postgres://user:pass@localhost/indelible"
export INDELIBLE_JWT_SECRET="$(openssl rand -hex 32)"
export INDELIBLE_ANTD_URL="http://localhost:8082"
./indelible
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
      INDELIBLE_ANTD_URL: http://antd:8082   # external antd container, not managed
      INDELIBLE_DB_URL: postgres://indelible:password@db/indelible
    depends_on:
      - db
      - antd

  db:
    image: postgres:16
    environment:
      POSTGRES_DB: indelible
      POSTGRES_USER: indelible
      POSTGRES_PASSWORD: password
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  indelible-data:
  pgdata:
```

## Key features

| Area | Capabilities |
|------|-------------|
| **Storage** | File upload/download, public/private visibility, cost estimation, background processing with retry |
| **Organization** | Collections (virtual folders), key-value tags, full-text tag search |
| **Auth** | Email/password, JWT sessions, API tokens with scoped permissions, OIDC/SSO, SCIM 2.0 provisioning |
| **Access control** | Users, groups, permission levels (read/write/admin), per-user quotas and file restrictions |
| **Wallets** | Multi-wallet support, encrypted key storage (AES-256-GCM), balance tracking, transaction history |
| **Observability** | Audit logs, system logs, upload/token/cost analytics, webhook notifications (generic + Slack) |
| **API quality** | Structured error codes, request ID tracing, rate limit headers, idempotency keys, webhook HMAC signatures, cursor pagination |

## API overview

Base path: `/api/v2`. Full Swagger docs available at `/api/docs/`.

```bash
# Register
curl -X POST /api/v2/auth/register \
  -d '{"email":"admin@example.com","password":"securepass","first_name":"Admin","last_name":"User"}'

# Upload a file
curl -X POST /api/v2/uploads \
  -H "Authorization: Bearer $TOKEN" \
  -F file=@document.pdf -F visibility=private

# List uploads with filtering
curl /api/v2/uploads?status=completed&sort=file_size:desc \
  -H "Authorization: Bearer $TOKEN"

# Create an API token
curl -X POST /api/v2/tokens \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"CI Pipeline","permissions":"[\"read\",\"write\"]"}'
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `INDELIBLE_JWT_SECRET` | **Required.** Secret for JWT signing | -- |
| `INDELIBLE_ANTD_URL` | antd daemon URL | `http://localhost:8082` |
| `INDELIBLE_ANTD_MANAGED` | Spawn and manage antd as child process | `false` |
| `INDELIBLE_ANTD_BIN` | Path to antd binary | `antd` (searches PATH) |
| `INDELIBLE_PORT` | HTTP listen port | `8080` |
| `INDELIBLE_DB_URL` | Database connection string | `sqlite://data.db` |
| `INDELIBLE_DATA_DIR` | Directory for temp files | `./data` |
| `INDELIBLE_WALLET_ENCRYPTION_KEY` | 64-char hex key for wallet encryption | -- |
| `INDELIBLE_BASE_URL` | External URL for email links | -- |
| `INDELIBLE_CORS_ORIGINS` | Comma-separated allowed origins | -- |
| `INDELIBLE_TRUSTED_PROXIES` | Comma-separated CIDR ranges | -- |

See the [User Guide](USER-GUIDE.md) for SMTP, debug, and advanced configuration.

## Project structure

```
cmd/indelible/         Entry point
internal/
  antd/                Managed antd process lifecycle
  config/              Configuration loading
  database/            Database init + migrations (SQLite + PostgreSQL)
  handlers/            HTTP handlers (auth, uploads, admin, SCIM)
  middleware/          Auth, rate limiting, maintenance, idempotency, SCIM auth
  services/            Business logic (users, uploads, tokens, webhooks, etc.)
  worker/              Background workers (upload processing, disk alerts, log retention)
  auth/                JWT + password hashing
  crypto/              AES-256-GCM encryption for wallet keys
web/src/               Vue 3 + TypeScript frontend
docs/                  Generated Swagger/OpenAPI specs
```

## Security

Indelible includes several layers of security hardening:

- **Wallet encryption** — private keys encrypted at rest with AES-256-GCM
- **JWT security** — HMAC-only algorithm enforcement, expiry validation, password-change invalidation
- **HTTP security headers** — CSP, X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Permissions-Policy
- **Request body limits** — 1MB limit on JSON endpoints (uploads have separate configurable limit)
- **Rate limiting** — login (5/60s), uploads (60/min), password reset (3/60s)
- **Upload validation** — path traversal prevention, configurable content-type allowlist, file size limits
- **Input sanitization** — parameterized SQL queries throughout, validated tag selectors

## Development

```bash
make dev          # Run Go + Vue dev servers in parallel
make test         # Run Go tests
make build        # Build frontend + backend
make check        # Run lint + test + security scan
make security     # Run govulncheck + npm audit
make fuzz         # Run fuzz tests (30s each)
make bench        # Run benchmark tests
```

### Bumping ant-sdk

indelible pins ant-sdk in two independent places:

- `.antd-version` — the daemon binary release tag (used by `release.yml` to download the `antd-*` artifact).
- `go.mod` — the `antd-go` Go module for the daemon call surface (`PrepareUpload`, `FinalizeUpload`, etc.).

Before bumping either, grep the actual symbols indelible consumes so you know what a breaking change in ant-sdk would touch:

```bash
grep -rn "antd\." internal/
```

That's the complete call surface — anything not listed there is a break you don't need to worry about. Then bump:

```bash
echo v0.3.0 > .antd-version                                              # daemon binary
go get github.com/WithAutonomi/ant-sdk/antd-go@$(cat .antd-version) \    # keep Go SDK in lockstep
  && go mod tidy
make test
```

Commit both files together so the daemon binary and the SDK never drift.

## CI pipeline

The CI runs on every PR and push to `master`:

| Job | What it checks | Blocks merge |
|-----|----------------|--------------|
| **Lint** | go vet, golangci-lint (8 linters), swagger drift | Yes |
| **Test** | `go test` — 50+ test files including workflow integration tests | Yes |
| **Frontend** | vue-tsc type check, vite build, vitest unit tests (35 tests) | Yes |
| **Race detection** | `go test -race` — detects data races | No (informational) |
| **Security** | gitleaks (secret scanning), govulncheck (Go vulns), npm audit | No (informational) |
| **E2E** | Playwright browser tests against full stack | Yes |

## Documentation

- **[User Guide](USER-GUIDE.md)** — setup, configuration, all features, API consumer guide, deployment
- **[Feature Specification](FEATURES.md)** — complete product spec with architecture decisions
- **Swagger** — interactive API docs at `/api/docs/` when running

## License

See [LICENSE](LICENSE) for details.
