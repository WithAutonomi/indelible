# Changelog

All notable changes to Indelible are documented in this file. This project adheres to [Semantic Versioning](https://semver.org/).

## [0.11.0] - 2026-06-18

This release tracks **antd / ant-sdk v0.10.0** (bundled daemon, Go client module, and image). Note that `v0.10.0` is the *antd* version; the Indelible release is `v0.11.0`.

### ⚠️ Breaking changes
- **Registration no longer auto-logs-in.** A successful `POST /auth/register` now returns `202 Accepted` with a neutral body and **no token or session cookie** (previously `201 Created` with a JWT). Clients must follow registration with an explicit login, and the response is identical whether the address is new or already registered (anti-enumeration). Only affects deployments that have self-registration enabled (off by default).
- **Webhook signatures are now replay-resistant.** The `X-Signature-256` HMAC is computed over `X-Webhook-Timestamp + "." + body` instead of `body` alone (header names unchanged). Consumers that verify signatures must recompute as `"sha256=" + hex(HMAC_SHA256(secret, timestamp + "." + raw_body))`; verifying against the body alone will now fail.

### Security & hardening
- **Encryption-key and JWT-secret rotation** via a key-id envelope and a `rotate-keys` CLI; former keys are kept verify/decrypt-only, so a rotation invalidates neither existing data nor live sessions.
- **Pluggable secrets-provider seam** (groundwork for external KMS / Vault backends; the default env-sourced backend is unchanged).
- **Constant-time login** to remove an account-existence timing side-channel.
- **Webhook delivery is SSRF-guarded** (outbound requests to loopback / private / link-local / metadata addresses are refused) and Slack `mrkdwn` is escaped.
- **`/health` diagnostics are admin-gated.** Liveness (`200`/`503` plus `status`/`database`/`antd`) is unchanged, but version, build commit, EVM network, payment-contract addresses, antd URL, and queue depth are now returned only to an authenticated admin.
- `X-Content-Type-Options: nosniff` on downloads; patched a transitive `form-data` advisory; new hardened-deployment guide for database encryption at rest.

### Reliability & payments
- **Bounded EVM payment allowances** and a **payment confirmation deadline** (`payment_confirmation_timeout_seconds`), so a stuck payment fails on a finite, operator-tunable timeout instead of hanging.
- **Paid-but-unfinished upload reconciliation**, plus a payment worker that caps merkle cost, frees stuck slots, and never abandons paid-for data.
- **Webhook dead-letter queue**: exhausted deliveries can be resent, and failed auth-link deliveries are escalated.

### Disaster recovery
- **Backup export/import of the upload catalog and DataMaps** — closes the path where losing the database meant permanently losing the only retrieval handle for private uploads.

### Horizontal read scaling (reader fleet)
- **`INDELIBLE_WORKERS_ENABLED` role flag** to split a deployment into one writer/worker plus N stateless reader replicas behind a load balancer.
- **Reader replicas boot without a wallet encryption key** (downloads don't pay).
- **Conditional GET for immutable content**: downloads carry a strong `ETag` / `Cache-Control: immutable` and honour `If-None-Match` with `304`, so a cache or CDN multiplies reader throughput.
- File-access reads are recorded in a dedicated `file_access_log`, split out of the tamper-evident audit hash-chain so concurrent readers can't fork it.
- New read-heavy (reader-fleet) deployment guide.

### Features
- **Dedicated cross-wallet Transactions page**, filterable.
- **User details drawer** — opens on whole-row click and shows groups, tokens, quota, and SSO identity.
- **Search results deep-link** to the entity page and open its detail drawer.
- **Data-directory disk-usage card** with a capacity pie chart.
- Auth forms carry autocomplete semantics so browsers offer password autofill.

### Fixes
- Collections list N+1 query and `collection_files` cascade-delete correctness.
- Devnet DX: accept the `local` network alias and track the renamed antd payment-vault manifest field.

### Platform & SDK
- **Bundled antd / ant-sdk updated to v0.10.0** (from v0.9.2) — the release trigger. The `antd-go` Go client module is bumped to v0.10.0 in lockstep (it had lagged at v0.8.0; the bump is API-additive — gRPC streaming download plus wallet / external-signer parity — and required no code changes). Bundled-image deployments get the new daemon automatically; external-signer operators should update their antd daemon to match.

[0.11.0]: https://github.com/WithAutonomi/indelible/releases/tag/v0.11.0

## [0.10.0] - 2026-06-04

### ⚠️ Breaking changes
- **Self-registration is now disabled by default.** Fresh deployments must provision the first administrator via `INDELIBLE_ADMIN_EMAIL` / `INDELIBLE_ADMIN_PASSWORD` (or `INDELIBLE_ADMIN_PASSWORD_FILE`). Existing deployments with users already present are unaffected.

### Security & hardening
- **Private file downloads now stream to disk** instead of buffering the entire file in memory, removing an authenticated out-of-memory / denial-of-service vector on large downloads. Downloads also gain a finite, operator-tunable timeout (`antd_download_timeout_secs`).
- **Go toolchain updated to 1.25.11**, clearing two standard-library vulnerabilities (`net/textproto`, `crypto/x509`).
- The **JWT signing secret must now be at least 32 bytes** at boot.
- **Webhook URLs are redacted** in logs and the audit trail.
- **Email addresses are validated** on registration, admin user-creation, and SSO provisioning.
- SPA authentication unified on **cookie sessions with CSRF protection**.
- Resolved frontend dependency advisories (`js-cookie`, `ws`).

### Audit & integrity
- **File-access events** (upload, download, delete, and denied attempts) are now recorded in the audit log.
- The **audit log is hash-chained and tamper-evident**, with a new integrity-verification endpoint.
- The audit-chain head can be **periodically anchored to Autonomi** for independent verification (opt-in, cost-gated).

### Features
- **Global search omnibox** (Ctrl/Cmd+K).
- **Dark mode** (deep-navy) with a theme toggle.
- **Date-range filtering** with quick presets on Uploads and Logs.
- **Version check** against the upstream release, plus antd version display in external-signer mode.
- **antd network-health** surfaced in the admin UI.
- Indelible logo mark across the favicon, auth pages, and sidebar.

### Fixes
- Administrators are **exempt from maintenance mode** so the off-switch stays reachable.
- Edit-user permission selector and status-banner dark-mode rendering.
- Quota management: entity picker, required-entity validation, and sub-gigabyte caps.
- Logs level/severity vocabulary and a Clear-filters control.
- Assorted production UI and upload-flow fixes.
- The disk critical-threshold pause is enforced when creating uploads.
- Bulk-tag requests are capped at 1000 upload IDs.
- **The antd daemon is now bundled into the container image**, shipping the corrected antd **v0.9.2** multi-arch build (tracks ant-client / ant-core) — resolves the Apple-Silicon (arm64) startup failure (exit code 133) (#85).

[0.10.0]: https://github.com/WithAutonomi/indelible/releases/tag/v0.10.0
