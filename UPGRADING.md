# Upgrading

Operator-facing notes for upgrades that need an action. Most upgrades are a
plain `docker compose pull && docker compose up -d` (data volumes persist) and
need nothing from this file — only the entries below call for a manual step.

## Unreleased

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
