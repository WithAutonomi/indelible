# Changelog

All notable changes to Indelible are documented in this file. This project adheres to [Semantic Versioning](https://semver.org/).

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
