# Scaling: read-heavy deployments (reader fleet)

Indelible normally runs as a **single all-in-one instance** — one process serving the API, the embedded SPA, and the background workers. That is the right default and handles a lot of traffic.

When your workload is **read-heavy** — content is uploaded over time but downloaded far more often (a back-catalogue served to many consumers) — you can scale the read path horizontally with a **read/write role split**:

- **One writer** instance: owns the EVM wallet, runs the background workers, and applies database migrations.
- **N reader replicas**: stateless, workers off, behind a load balancer. They serve HTTP — chiefly downloads — and nothing else privileged.

All instances share **one PostgreSQL database**. Content bytes come from the Autonomi network, not from the writer, so any reader can serve any file.

> Downloads are free on Autonomi (you pay to upload, not to retrieve), so adding readers adds serving capacity without adding network cost.

## When to use this

Use the split when download traffic is the bottleneck and a single instance can't keep up. If uploads are your bottleneck, this does **not** help — uploads are processed by the single writer by design (see below). For most deployments the single all-in-one instance is simpler and sufficient.

## Topology

```
                    ┌─────────────┐
   consumers ─────► │ load        │ ─────► reader replica 1 ─┐
   (hold an API     │ balancer    │ ─────► reader replica 2 ─┤
    token)          └─────────────┘ ─────► reader replica N ─┤
                                                             ├──► PostgreSQL (primary)
   admin / uploads ───────────────────────► writer ─────────┘
                                              │ owns wallet, workers, migrations
                                              └──► EVM network (payments)
```

## Roles and configuration

| | Writer (1) | Reader (N) |
|---|---|---|
| `INDELIBLE_WORKERS_ENABLED` | `true` (default) | `false` |
| Wallet encryption key | **required** | not required |
| `INDELIBLE_JWT_SECRET` | required | **required** (verifies sessions/tokens) |
| `INDELIBLE_DB_URL` | shared PostgreSQL | same shared PostgreSQL |
| antd | required | required (can be wallet-less / read-only, co-located per pod) |
| Runs workers + migrations | yes | no |

A reader is started with `INDELIBLE_WORKERS_ENABLED=false`. In that mode it:

- starts **no background workers** (upload processing, log retention, disk alerts, audit anchoring, system monitor, idempotency cleanup);
- **skips database migrations** (the writer owns the schema);
- needs **no wallet encryption key** — it never decrypts an EVM wallet or OIDC secret. Access control does not use that key: API tokens are validated by a database lookup, and session JWTs by `INDELIBLE_JWT_SECRET`.

Readers still need the shared database, `INDELIBLE_JWT_SECRET`, and an antd daemon to fetch bytes from the network.

## The one-writer rule (important)

Run **exactly one** writer (workers-enabled) instance. The worker tier owns operations that are unsafe to run from more than one process against the same wallet and database:

- **EVM nonce management** — two signers on one wallet produce nonce collisions (double-spends / rejected transactions).
- **Audit-anchor payments** — each worker would pay ANT + gas to anchor the same chain head.
- **Upload-queue processing** — the same upload could be dequeued and processed twice.
- **Audit hash-chain writes** — serialized per process; two writers would fork the tamper-evident chain.

There is **no leader election or fencing** today, so "exactly one writer" is an operational invariant you enforce by deployment — not something the software arbitrates. Do not autoscale the writer, and ensure a blue/green or rolling deploy never runs two writers concurrently.

## Where privileged traffic must go

Route **uploads and admin/privileged operations to the writer**, not to readers:

- **Uploads** require the wallet (payment) — only the writer has it.
- **Wallet management** and **OIDC provider configuration** encrypt secrets at rest. A key-less reader refuses these with `503 Service Unavailable` (so it can't seal data under a throwaway key that the writer couldn't read back).
- **SSO/OIDC login** decrypts the OIDC client secret, which needs the wallet key — terminate it on the writer.

Readers handle the read surface: downloads, listing, search, and **API-token / password-session authentication** (both validated against the shared database — no wallet key involved).

## Migrations and deploy ordering

Only the writer runs migrations. On upgrade, **deploy the writer first** (it applies the new schema), then roll the readers. A reader that boots before the writer has migrated will run against an older schema.

## Caching (the biggest lever)

Downloaded content is **immutable and content-addressed**, so download responses carry a strong `ETag` and `Cache-Control: private, max-age=31536000, immutable`, and honour `If-None-Match` with `304 Not Modified` (skipping the network fetch). Put a cache in front of the readers to multiply throughput:

- a **trusted-boundary reverse-proxy cache** keyed on the request after authentication, or
- the **customer's own frontend / CDN** downstream of its API token.

Responses are marked `private` because downloads are token-gated (there is no anonymous route), so a shared public cache must not reuse a response across identities.

## Load balancer notes

- Use `/health` for readiness/liveness probes.
- Set `INDELIBLE_TRUSTED_PROXIES` (CIDR ranges) so client IPs are read from `X-Forwarded-For` for rate limiting and audit logging. Without it, the proxy's IP is used.
- Readers are stateless — no session affinity is required for authentication (sessions are stateless JWTs; tokens are database-backed).

## Multiple datacentres

Readers **can** run in different datacentres — the content they serve comes from the global Autonomi network, not from the writer. The limiter is the **shared PostgreSQL**: every request does a metadata/auth read against the database, so a reader far from the primary pays cross-DC latency on each request. Today Indelible uses a single database endpoint (all reads hit the primary); routing reads to a local PostgreSQL read replica is a planned enhancement. For now, keep readers and the PostgreSQL primary in the same region (or on a low-latency interconnect), and make PostgreSQL itself highly available separately (managed Multi-AZ, streaming replication + connection pooling). The writer stays in one region.

## Operational caveats

Some state is per-instance and will drift mildly across the fleet (none of it corrupts data):

- **Rate limiting** is in-memory per instance — the effective limit is roughly `per-instance-limit × instance-count`. Use a shared limiter at the load balancer if you need a precise global cap.
- The **runtime settings cache** has a short per-instance TTL, so a settings change (e.g. `maintenance_mode`, `registration_enabled`) can take effect on different instances a few seconds apart.
- **System-monitor alerts** dedupe per instance — only the writer runs the monitor, so this is not a concern in the standard split.

## Database

PostgreSQL is **required** for the split — SQLite is single-node (single-writer) and cannot back multiple instances. Point every instance at the same PostgreSQL, and make that database highly available on its own (managed service or streaming replication with a pooler such as PgBouncer).
