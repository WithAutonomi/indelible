# Upgrading

Operator-facing notes for upgrades that need an action. Most upgrades are a
plain `docker compose pull && docker compose up -d` (data volumes persist) and
need nothing from this file — only the entries below call for a manual step.

## Unreleased

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
