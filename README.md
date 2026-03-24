# Indelible

Enterprise storage gateway for the [Autonomi](https://autonomi.com) decentralized network. Upload, manage, and retrieve files on permanent decentralized storage with a familiar REST API, admin dashboard, and enterprise identity integrations.

## What it does

Indelible sits between your applications and the Autonomi network. It handles authentication, file management, cost estimation, wallet payments, and audit logging — so you don't have to interact with the network directly.

- **Single binary** — Go backend + Vue 3 frontend, embedded. One process, no external dependencies beyond a database.
- **SQLite or PostgreSQL** — SQLite by default for zero-config local use; PostgreSQL for production scale.
- **REST API** — full CRUD for uploads, collections, tags, tokens, users, groups, and admin operations. Swagger docs at `/api/docs/`.
- **Admin dashboard** — user management, wallet configuration, quotas, webhooks, OIDC providers, SCIM provisioning, analytics, and audit logs.
- **Enterprise identity** — OIDC/SSO with any provider (Okta, Azure AD, Google), SCIM 2.0 for automated user/group provisioning.

## Quick start

**Prerequisites:** [antd](https://github.com/WithAutonomi/ant-sdk) daemon running (handles all Autonomi network operations).

```bash
# Build
go build -o indelible ./cmd/indelible

# Run with defaults (SQLite, port 8080)
export INDELIBLE_JWT_SECRET="$(openssl rand -hex 32)"
export INDELIBLE_ANTD_URL="http://localhost:8081"
./indelible
```

Open http://localhost:8080 — the first user to register becomes admin.

### With a config file

```bash
./indelible --config indelible.toml
```

```toml
port = 8080
db_url = "sqlite://./data.db"
antd_url = "http://localhost:8081"
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
export INDELIBLE_ANTD_URL="http://localhost:8081"
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
      INDELIBLE_ANTD_URL: http://antd:8081
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
| `INDELIBLE_ANTD_URL` | antd daemon URL | `http://localhost:8081` |
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

## Documentation

- **[User Guide](USER-GUIDE.md)** — setup, configuration, all features, API consumer guide, deployment
- **[Feature Specification](FEATURES.md)** — complete product spec with architecture decisions
- **Swagger** — interactive API docs at `/api/docs/` when running

## License

See [LICENSE](LICENSE) for details.
