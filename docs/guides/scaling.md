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
| antd | required (with wallet, for uploads/payments) | **its own, co-located, wallet-less** — see below |
| Runs workers + migrations | yes | no |

A reader is started with `INDELIBLE_WORKERS_ENABLED=false`. In that mode it:

- starts **no background workers** (upload processing, log retention, disk alerts, audit anchoring, system monitor, idempotency cleanup);
- **skips database migrations** (the writer owns the schema);
- needs **no wallet encryption key** — it never decrypts an EVM wallet or OIDC secret. Access control does not use that key: API tokens are validated by a database lookup, and session JWTs by `INDELIBLE_JWT_SECRET`.

Readers still need the shared database, `INDELIBLE_JWT_SECRET`, and an antd daemon to fetch bytes from the network.

## Each instance runs its own antd (co-located) — required, not optional

Every instance — the writer and **each** reader — must have its **own antd daemon on the same host / in the same pod, sharing a filesystem** with the indelible process. This is a hard requirement of how downloads work, not a tuning preference:

- A download is streamed via antd's `dest_path` contract. Indelible hands antd a **local temp path** (under `INDELIBLE_DATA_DIR`), antd fetches the file from the network and **writes the bytes to that path**, and indelible then serves that file from disk. So antd and indelible must see the **same filesystem**.
- This means you **cannot point N readers at one shared remote antd** over HTTP: a remote antd would write `dest_path` on *its own* disk, and the reader's file-serve would find nothing. Co-locate antd with each indelible instance and share the temp volume (`INDELIBLE_DATA_DIR`).

Practical shape of a reader pod: `indelible` (workers off, no wallet key) **+** its own `antd`, sharing a volume mounted at `INDELIBLE_DATA_DIR`.

A reader's antd can be **wallet-less / read-only**: downloads (`FileGet` / `FileGetPublic`) don't pay, so the reader's antd needs no funded wallet. Each reader's antd also fetches from the network independently, so readers scale in parallel with no shared-antd bottleneck. The **writer's** antd is the one that needs the wallet (uploads pay for storage).

In managed mode (`INDELIBLE_ANTD_MANAGED=true`) indelible spawns and supervises its own antd child process, which satisfies the co-location requirement automatically. In external mode, point each instance's `INDELIBLE_ANTD_URL` at its **local** antd, not a shared one.

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

### The built-in download cache

Independent of any front cache, each instance can keep a **local disk cache of public download bytes** (`download_cache_max_bytes`, off by default). A cache hit is served straight from the instance's disk: no antd fetch, no temp copy, no admission-gate slot. Full sizing, telemetry, and privacy guidance lives in the [download-cache deployment guide](download-cache.md).

Fleet notes:

- The cache is **per instance** — every reader warms and evicts its own copy from its own traffic. With no load-balancer affinity, popular objects end up replicated on every reader; at typical working-set sizes (small hot objects) that is the right trade, and it is what sizing the budget per instance assumes.
- The `download_cache_*` settings are **fleet-global values applied per instance** (readers share the writer's database). For a fleet with **different disk sizes per reader**, set the env override `INDELIBLE_DOWNLOAD_CACHE_MAX_BYTES` on the instances that differ — env beats the DB setting on that instance only, and `0` disables its cache outright.
- The cache can never be the reason uploads pause: a background sweeper evicts it — aggressively, toward empty if necessary — as soon as the data volume approaches the disk-alert worker's critical threshold, and otherwise keeps it under budget by LRU with an optional inactivity window (`download_cache_inactive_secs`).
- **Upload seeding is writer-local.** `download_cache_seed_on_upload` (default on) promotes a public upload's staged bytes into the cache when the store succeeds — but uploads only pass through the writer, so on a role-split fleet this warms the **writer's** cache, not the readers'. Readers warm by read-through: the first download per reader is still a cold fetch. The full publish-then-read win applies to all-in-one deployments, where writer and reader are the same process.

## Load balancer notes

- Use `/health` for readiness/liveness probes.
- Set `INDELIBLE_TRUSTED_PROXIES` (CIDR ranges) so client IPs are read from `X-Forwarded-For` for rate limiting and audit logging. Without it, the proxy's IP is used.
- Readers are stateless — no session affinity is required for authentication (sessions are stateless JWTs; tokens are database-backed).

## Multiple datacentres

Readers **can** run in different datacentres — the content they serve comes from the global Autonomi network, not from the writer. The limiter is the **shared PostgreSQL**: every request does a metadata/auth read against the database, so a reader far from the primary pays cross-DC latency on each request. Today Indelible uses a single database endpoint (all reads hit the primary); routing reads to a local PostgreSQL read replica is a planned enhancement. For now, keep readers and the PostgreSQL primary in the same region (or on a low-latency interconnect), and make PostgreSQL itself highly available separately (managed Multi-AZ, streaming replication + connection pooling). The writer stays in one region.

## Operational caveats

Some state is per-instance and will drift mildly across the fleet (none of it corrupts data):

- **Rate limiting** is in-memory per instance — the effective limit is roughly `per-instance-limit × instance-count`. Use a shared limiter at the load balancer if you need a precise global cap.
- The **download admission gate** (`max_concurrent_downloads`) is likewise in-memory per instance — effective fleet download concurrency is roughly `max_concurrent_downloads × instance-count`, not one global cap. That is usually what you want (the resources it protects — temp disk and the antd fetch budget — are per-instance too); size the setting for one instance's disk and bandwidth, not for the fleet.
- The **runtime settings cache** has a short per-instance TTL, so a settings change (e.g. `maintenance_mode`, `registration_enabled`) can take effect on different instances a few seconds apart.
- **System-monitor alerts** dedupe per instance — only the writer runs the monitor, so this is not a concern in the standard split.

## Database

PostgreSQL is **required** for the split — SQLite is single-node (single-writer) and cannot back multiple instances. Point every instance at the same PostgreSQL, and make that database highly available on its own (managed service or streaming replication with a pooler such as PgBouncer).
