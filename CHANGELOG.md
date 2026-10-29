# Changelog

All notable changes to Indelible are documented in this file. This project adheres to [Semantic Versioning](https://semver.org/).

## [0.13.0] - 2026-10-29

This release tracks **antd / ant-sdk v0.17.0** (bundled daemon, Go client module, and image). Indelible now needs **Go 1.26.10** to build.

### Upload payments: no double payment, multi-batch merkle, resumable finalize

- **Multi-batch merkle uploads work** (#162, V2-1056). Since antd v0.12.0, an upload larger than one merkle tree (256 fresh chunks, about 1 GiB) is split into several payment batches. v0.12.0 didn't read them, sent an empty payment plan, and failed without paying. Each batch is now paid with its own `payForMerkleTree` transaction and recorded before the next, and the upload finalizes once all are paid. A definitive failure partway through stores what was paid for and keeps the upload for recovery. Verified on devnets with antd v0.12.0 and v0.17.0, each with a 1.15 GiB upload paid in two batches.
- **A live upload is never requeued or paid for twice** (#170, V2-1773). The stuck-upload sweep used to put an upload back in the queue after 60 minutes in "processing", even while a worker was still paying for or storing it, and the next poll paid for it again. Now:
  - the sweep never requeues an upload this process is working on;
  - progress heartbeats (after prepare, after each payment, before finalize) make the window measure time without progress;
  - no automatic re-run pays again for an upload that has already paid since it was created or last retried by an admin. It stops as `paid_unfinalized` with its source kept, and an admin **Retry** authorizes one new payment;
  - if the ledger write for a confirmed payment fails, the rest of that attempt can't retry or pay again.
- **A partial finalize is resumed against the payment antd kept** (#171, V2-1774; antd ≥ 0.14). When antd reports that only some chunks were stored and it kept the paid attempt, the worker repeats the finalize with the same upload ID. It makes up to five repeats over about 7.5 minutes, and stops early once the failed-chunk count stops shrinking. Nothing is paid again.
- **Uploads of content that's already on the network complete again** (#172, V2-1775). When every chunk was already stored, the finalize sent `"tx_hashes": null`, which antd rejects, so the upload failed even though its data was on the network. This also unblocks the admin Retry of an upload whose store antd finished after a crash.

### Admin

- **Upload requeue window** (#170): `GET`/`PUT`/`DELETE /api/v2/admin/upload-requeue-window` and an "Upload queue" card on the System page.
  - It sets how long an upload may make no progress before the sweep requeues it. The default is 60 minutes and the range is 1 minute to 7 days.
  - An override lasts until restart, every change is audited (`upload_requeue_window_changed`), and reader instances refuse changes with 409.

### Behaviour changes

- **A wave-batch finalize failure no longer pays again for its unstored chunks** (#170). It used to re-prepare and pay for them. Now it first resumes against the kept payment (#171), and if that can't finish, it ends as `paid_unfinalized` for an admin Retry.
- **Not-found downloads return 404 `antd_not_found`** instead of 502 `antd_error` (antd v0.14.0).

### Licensing

- **Releases and the image ship licence texts and third-party notices** (#165). Each release binary comes in an archive with `LICENSE-MIT`, `LICENSE-APACHE` and `THIRD-PARTY-NOTICES.txt`, and the image carries them under `/usr/share/doc/indelible/`. The notices include the LGPL section for the statically linked go-ethereum packages: its exact version, where to get the source, and how to relink.

### Security & dependencies

- **Go 1.26.10** for GO-2026-6809 (`crypto/x509`). Go 1.26 is now required to build, because antd-go v0.15.0 needs it.
- **Web dependencies** (#168): `npm audit fix` clears the high-severity production advisories: vue 3.5.44, axios 1.20.0, follow-redirects 1.16.1, source-map-js 1.2.2.
- The bundled antd v0.17.0 includes the rustls-webpki emSign advisory fixes (RUSTSEC-2026-0381 to -0383).

### Known issue

- **After a network outage of a minute or more, antd can stay disconnected for up to about 13 minutes** (V2-1778, upstream). During that window, uploads fail before paying, while `/health` still reports `write_ready`. A partial finalize caused by the outage can't be resumed in that window either, so it ends `paid_unfinalized`. Retry it once antd reconnects, or restart antd to reconnect at once.

### Internal

- Bundled antd moved v0.12.0 → v0.15.0 → v0.16.0 → v0.17.0 across all three lockstep pins (`antd-go` module, `.antd-version`, Docker `ANTD_IMAGE`) (#166, #167, #169).
- Migration 015 adds `uploads.payment_floor_txn_id`.
- Docs: removed the stale compose comment claiming the upstream antd image can't exec its binary (#164).

[0.13.0]: https://github.com/WithAutonomi/indelible/releases/tag/v0.13.0

## [0.12.0] - 2026-08-17

This release tracks **antd / ant-sdk v0.12.0** (bundled daemon, Go client module, and image). The matching version numbers are a coincidence — the two projects are versioned independently.

### Download cache (read acceleration)

A new per-instance, content-addressed download cache turns the read path into a three-layer degradation: `304` (ETag match) → **cache hit** (local bytes, Range preserved, no antd fetch, no gate slot) → gated miss (fetch, then promote the temp file the download already paid to materialise). **Off by default** — set `download_cache_max_bytes` to enable; see the new deployment guide at `docs/guides/download-cache.md`.

- **Cache core** — atomic rename promotion (no partially-written file is ever visible), boot scan rebuilds the index with no DB writes (reader-fleet discipline), per-key fill coalescing so concurrent misses on one object produce a single antd fetch.
- **Disk policy sweeper** — a per-minute pass in strict precedence order: disk pressure (the cache is sacrificed, toward empty if needed, before disk usage can pause uploads), optional inactivity window (`download_cache_inactive_secs` — also the privacy dial bounding how long cached plaintext lingers), then LRU eviction to the byte budget with admission headroom. Runs on every role, readers included.
- **Write-through seeding** (`download_cache_seed_on_upload`, default on) — after a public upload completes, the staged temp file is renamed into the cache instead of deleted, so publish-then-read is served from local disk with zero extra I/O. Warms the writer's cache only; readers warm by read-through.
- **Private-content caching opt-in** (`download_cache_private`, default off) with **fleet-wide delete purge**: deletes append the upload's cache keys to a `cache_purge_log` (migration 013) consumed by every instance's sweep tick, with boot reconciliation covering deletes that happen while an instance is down. The purge-window contract: the deleting instance purges synchronously (a reported deletion never leaves pre-existing cached plaintext readable — the API fails closed instead), every other instance purges within ~one 60s sweep tick.
- **Observability** — lifecycle counters across serve/admission/coalescing/eviction/purge, emitted as a periodic `download cache stats` log line only when something changed, with a telemetry table in the guide mapping each stat to the action it should trigger.
- Settings: `download_cache_max_bytes` (master switch; `INDELIBLE_DOWNLOAD_CACHE_MAX_BYTES` overrides per instance for heterogeneous fleets), `download_cache_max_object_bytes`, `download_cache_min_uses`, `download_cache_inactive_secs`, `download_cache_seed_on_upload`, `download_cache_private` — all live-tunable, all in the admin UI.

### Download admission control

- **`max_concurrent_downloads`** (default 8) bounds concurrent downloads for the whole request, so download temp disk is deterministically bounded and bulk-download bursts can no longer flip the disk-pressure uploads pause or collectively starve the per-file fetch timeout. Excess requests wait up to **`download_queue_wait_secs`** (default 30) for a slot, then get `503` with `{"code":"downloads_saturated"}` and a `Retry-After` hint. The `304` conditional path and cache hits stay outside the gate.

### Security & hardening

- **Removed chi's `RealIP` middleware** (GO-2026-5777, Critical): it rewrote `RemoteAddr` from the forgeable `X-Forwarded-For` before the trusted-proxy logic ran, making per-IP rate limits bypassable and logged IPs spoofable by any direct client. `X-Forwarded-For` is now honored exclusively by the trusted-proxy-gated resolver (`trusted_proxies`); chi bumped to v5.3.0.
- **Audit, file-access, and settings-change logs record the resolved client IP** — `X-Forwarded-For` honored only from peers inside `trusted_proxies` — restoring real client attribution behind a reverse proxy, safely.
- Toolchain and dependency advisories cleared: Go 1.25.13 (six stdlib advisories incl. `crypto/tls`, `net/http`, `net/url`), `golang.org/x/net` v0.55.0, `google.golang.org/grpc` 1.82.1, chi 5.3.0, vite 8 (esbuild gone from the tree entirely), axios 1.18.1, nanoid, brace-expansion.

### Fixed

- **Duplicate re-uploads no longer fail at the final step.** The `already_stored` status (content-addressed dedup — re-uploading a file whose chunks are all already on the network) was written by the upload worker but missing from the database schema's status constraint, so the transition was rejected after the network store succeeded and the upload surfaced as "Failed to save upload record". Migration 014 fixes the constraint in both dialects, and such uploads are now also deletable like any other stored upload.
- **Operator note:** uploads that hit this bug before the fix sit in `failed` state; migration 014 does not rewrite them. Retry them once (the re-Prepare is a zero-cost dedup) and they will complete as `already_stored`.
- **Deleting an upload purges its cached bytes** on the handling instance before the row is removed (fails closed if the unlink fails), with resurrection guards closing the promote-vs-delete race on every instance.

### Internal

- Bundled antd moved v0.10.0 → v0.12.0 across all three lockstep pins (`antd-go` module, `.antd-version`, Docker `ANTD_IMAGE`).
- CI: heavy jobs skip on documentation-only changes (required Test contexts still reported); release artifact download scoped to the binaries.
- Docs: runtime-settings table reconciled with the actual settings surface; new download-cache deployment guide; reader-fleet scaling guide updated for cache warm-up semantics.

[0.12.0]: https://github.com/WithAutonomi/indelible/releases/tag/v0.12.0

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
