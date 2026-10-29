# Upgrading

Operator-facing notes for upgrades that need an action. Most upgrades are a
plain `docker compose pull && docker compose up -d` (data volumes persist) and
need nothing from this file — only the entries below call for a manual step.

## v0.12.0 — 2026-08-17

### Bundled antd updated to v0.12.0

The bundled antd daemon (and the `antd-go` client module) move to **v0.12.0**. Container
deployments get the new daemon automatically on `docker compose pull`. **If you run antd
yourself**, update your antd daemon to v0.12.0 so it stays in lockstep with the binary
Indelible was built against.

### Reverse-proxy deployments: set `trusted_proxies` (log/rate-limit attribution change)

The spoofable `RealIP` middleware was removed (GO-2026-5777). `X-Forwarded-For` is now
honored **only** when the connecting peer is listed in `trusted_proxies`. Behind nginx/caddy:

- **With `trusted_proxies` configured** (as the deployment guide recommends): audit and
  file-access logs now record the true client IP, and per-IP rate limiting works as designed.
  No action beyond confirming the setting.
- **Without it**: logs record the proxy's address and rate limiting keys on the proxy —
  add your proxy addresses to `trusted_proxies` to restore client attribution.

Direct (no-proxy) deployments need nothing; forged `X-Forwarded-For` headers from clients
were never trusted by the rate limiter and are now ignored everywhere else too.

### Database migrations 013 + 014 (automatic)

Both apply on first boot as usual (no manual step): 013 adds the download-cache purge log
and `uploads.cache_key`; 014 admits the `already_stored` status. One post-upgrade action:
**uploads that previously failed with "Failed to save upload record"** (a duplicate-content
re-upload hitting the pre-014 constraint) sit in `failed` state — retry each once; the
re-Prepare is a zero-cost dedup and they will complete as `already_stored`.

### Download cache is available but off by default

Nothing changes unless you opt in. To enable the read cache, set `download_cache_max_bytes`
(admin UI → Transfer Limits, or the `INDELIBLE_DOWNLOAD_CACHE_MAX_BYTES` per-instance
override) and read `docs/guides/download-cache.md` for sizing, fleet scope of each dial,
and the privacy posture — especially before enabling `download_cache_private`.

## v0.11.0 — 2026-06-18

### Bundled antd updated to v0.10.0

The bundled antd daemon (and the `antd-go` client module) move to **v0.10.0**. Container
deployments get the new daemon automatically on `docker compose pull`. **If you run antd
yourself (external-signer mode)**, update your antd daemon to v0.10.0 so it stays in lockstep
with the binary Indelible was built against.

### Webhook signatures are now replay-resistant (action for webhook consumers)

The `X-Signature-256` HMAC is now computed over `X-Webhook-Timestamp + "." + body` (Stripe-style)
instead of over `body` alone. Header names are unchanged. **If any system verifies webhook
signatures**, update verification to:

    expected = "sha256=" + hex(HMAC_SHA256(secret, X-Webhook-Timestamp + "." + raw_body))

(and reject stale timestamps for replay protection). Verifying against the body alone will now
reject every delivery.

### Webhook and OIDC egress is SSRF-guarded

Outbound webhook deliveries — and OIDC issuer discovery — now refuse to connect to loopback,
private (RFC1918/ULA), link-local, and cloud-metadata addresses. **If a configured webhook
target or your OIDC issuer resolves to a private/internal IP** (including a public hostname that
resolves internally via split-horizon DNS), those calls will now fail. Point them at a
publicly-resolvable address, or keep the integration off this instance.

### /health no longer exposes diagnostics anonymously

Liveness is unchanged — `/health` still returns `200`/`503` with `status`/`database`/`antd` to
any caller, so uptime probes keep working. The detailed fields (version, build commit, EVM
network, payment-contract addresses, antd URL, queue depth) are now returned **only to an
authenticated admin**. Repoint any monitoring that scraped those detail fields anonymously.

### Registration no longer logs the user in

Only relevant if you have enabled self-registration: a successful `POST /auth/register` now
returns `202` with a neutral body and **no token or session** — the client must follow with an
explicit login. See the self-registration note below.

### File download events moved to a separate File Access log

`file_downloaded` and `file_download_denied` events are no longer written to the
`audit_log` — they now go to a new `file_access_log` table (a plain, append-only
log), surfaced under **Admin → Logs → File Access** and at
`GET /api/v2/admin/logs/file-access` (`/export`, `/stats`). This keeps
high-volume download telemetry off the tamper-evident audit hash-chain so it can
scale across multiple instances.

**Action:** none required to upgrade (the migration adds the new table
automatically). The change is **forward-only** — download rows already in
`audit_log` stay there, so the audit chain is untouched. **If you have external
tooling that reads `file_downloaded`/`file_download_denied` from the audit log
or its export**, repoint it at the File Access log endpoint. File **upload** and
**delete** events are unchanged (still in the audit log).

### Optional: stateless reader replicas (read-heavy scaling)

A new `INDELIBLE_WORKERS_ENABLED` flag (default `true`) lets you run extra
HTTP-only "reader" replicas with the background workers off, alongside a single
"writer" that owns the wallet, workers, and migrations. No action is required —
the default keeps the existing all-in-one behaviour. To scale out, see the
read/write split notes (reader replicas need shared PostgreSQL; the writer stays
a single instance).

### Self-registration is disabled by default (security)

`POST /auth/register` used to be open to anyone and granted every new user
read access. With the coarse access model (any read user can list/download
everything), that meant anyone who could reach the instance could read all
uploads. The first registrant was also auto-promoted to admin. Both are fixed:

- The first administrator is now **seeded from config**, not from the first
  registration. Set `INDELIBLE_ADMIN_EMAIL` and `INDELIBLE_ADMIN_PASSWORD`
  (or `INDELIBLE_ADMIN_PASSWORD_FILE` for Docker/Kubernetes secrets, which
  takes precedence). The seed runs once, only while no administrator exists.
- The server **refuses to start** if it has no administrator and no seed is
  configured — a fresh instance can no longer come up in an unusable state.
- Self-registration is **off by default**; when an admin enables it, new users
  receive **read-only** access (never admin).

**New installs:** set the two `INDELIBLE_ADMIN_*` variables before first boot
(the shipped `docker-compose.yml` requires them). Log in with those credentials.

**Existing installs:** no action is required to keep running — you already have
an administrator, so the server boots normally and the seed variables are
ignored. **If you relied on open self-registration**, note that new sign-ups
will now receive `403`. To restore it, an admin enables it in
**Admin → Settings** (`registration_enabled = true`).
