# Upgrading

Operator-facing notes for upgrades that need an action. Most upgrades are a
plain `docker compose pull && docker compose up -d` (data volumes persist) and
need nothing from this file — only the entries below call for a manual step.

## v0.13.0 — 2026-10-29

### Bundled antd updated to v0.17.0

The bundled antd daemon (and the `antd-go` client module) move from v0.12.0 to **v0.17.0**.
Container deployments get the new daemon automatically on `docker compose pull`. **If you
run antd yourself**, update your antd daemon to v0.17.0 so it stays in lockstep with the
binary Indelible was built against. The resume of partial finalizes (#171) needs antd ≥ 0.14.

Building from source now needs **Go 1.26.10** or later.

### Database migration 015 (automatic)

It applies on first boot as usual. It adds `uploads.payment_floor_txn_id`, which marks which
payments count against an upload's current authorization to pay. The migration fills it in
so that each existing upload keeps the paid/not-paid answer it had before. No manual step is
needed.

### `paid_unfinalized` uploads: Retry is the way to pay again

Indelible no longer pays automatically a second time for an upload whose payment it has
recorded. That includes a wave-batch upload whose finalize failed, which v0.12.0
re-prepared and paid for again. Such an upload first resumes against the payment antd kept.
If that can't finish, it stops as `paid_unfinalized` with its source file kept.

- **Retry** (upload list or `POST /api/v2/uploads/{id}/retry`) authorizes one new payment.
  Chunks that are already stored cost nothing, so a Retry usually pays only for what is
  missing.
- If your automation watched for uploads being retried and paid automatically, expect
  `paid_unfinalized` instead.
- **One gap remains:** if Indelible crashes after a payment confirms on-chain but before it
  is recorded, the guard can't see that payment. The restarted worker can then pay again for
  chunks that aren't stored yet. After a crash during uploads, reconcile the wallet's on-chain
  transactions against the upload ledger.

### Long uploads are no longer requeued mid-flight

Before this release, an upload that spent more than 60 minutes in "processing" was put back
in the queue while its worker was still running, and was then paid for again. Now an upload
a worker is still processing is never requeued, however long a phase takes. Past the window
it is only logged as a warning. The window (60 minutes by default) now affects only uploads
that no worker is processing. No action is needed. To quiet the warning for routinely long
phases, raise the window on the System page ("Upload queue") or with
`PUT /api/v2/admin/upload-requeue-window`. The override lasts until the next restart.

### Not-found downloads: 404 instead of 502

A download of something that isn't on the network now returns **404 `antd_not_found`**
(antd v0.14.0) instead of 502 `antd_error`. Update any client or monitor that treats that 502
as "not found".

### Licence files in releases and the image

Release binaries now come in archives together with `LICENSE-MIT`, `LICENSE-APACHE` and
`THIRD-PARTY-NOTICES.txt` (the bare binaries are still attached). The image carries the same
files under `/usr/share/doc/indelible/`. If you redistribute Indelible, ship these files with it.

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
