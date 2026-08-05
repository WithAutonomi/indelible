# The download cache — deployment guide

Indelible can keep a per-instance disk cache of **public** download bytes, so repeat reads (and, with seeding, the very first read after publishing) are served from local disk instead of re-fetched from the Autonomi network. This guide covers how the pieces layer, how to size the cache, what its telemetry means, and the exact scope of every dial. For a one-line-per-setting reference see the settings table in the [User Guide](../../USER-GUIDE.md).

The cache is **off by default** — it activates when you give it a byte budget (`download_cache_max_bytes`).

## How a download is served (the three layers)

A download request passes through up to three layers, cheapest first:

1. **ETag / `304 Not Modified`.** Content is immutable and content-addressed, so responses carry a strong `ETag` and `Cache-Control: private, max-age=31536000, immutable`. A client (or trusted-boundary proxy) that already holds the bytes revalidates with `If-None-Match` and gets a `304` — no disk, no network, no gate slot.
2. **Cache hit.** If the object is in the local cache, it is served straight from disk — **bypassing the download admission gate** (`max_concurrent_downloads`), because a hit consumes neither of the resources the gate protects (temp-disk staging and the antd fetch budget). Hits are also why a saturated instance keeps serving its hot set even while cold fetches queue.
3. **Gated miss.** Otherwise the request takes a gate slot, fetches from antd to a temp file, streams to the client — and, if eligible, promotes those bytes into the cache on the way (an atomic rename, not a copy). Concurrent misses on the same object are coalesced: one fetch fills, the rest wait and serve the filled entry.

## What gets cached

- **Public uploads only** (privacy posture below).
- Objects at or under `download_cache_max_object_bytes` (default 64 MiB) — large files stay in the streaming regime.
- On the read path, objects that have been requested at least `download_cache_min_uses` times (default 1). Production traces show a median of ~72% of objects in a cache-sized window are never re-read; raising min-uses to 2–3 keeps those one-hit-wonders from displacing hot content.
- On the upload path (`download_cache_seed_on_upload`, default on): a public upload's staged bytes are promoted the moment its network store succeeds, deliberately **skipping** the min-uses bar — publish-then-read is the bet seeding makes, and the sweeper evicts wrong bets.

## Keeping it under control (the sweeper)

A background sweeper runs on **every** role — readers included — once a minute:

1. **Disk pressure wins over everything.** At 90% volume usage it evicts aggressively, toward empty if needed, so the cache is always sacrificed before the disk-alert worker has to pause uploads at 95%. Cached bytes are reconstructible acceleration; upload staging is durability in flight.
2. `download_cache_inactive_secs` (optional): entries not accessed within the window are deleted regardless of budget.
3. **Budget**: least-recently-used entries are evicted until the cache sits ~10% under `download_cache_max_bytes`, leaving admission headroom so new, hotter objects can always displace the LRU tail.

Setting the budget to `0` doesn't just stop admissions — the sweeper drains existing entries to empty disk, so "disabled" converges to no plaintext lying around.

## Sizing and reading the telemetry

The sweep worker emits a cumulative `download cache stats` line (JSON, stdout) whenever the counters move. How to read it:

| Signal | Meaning | Action |
|---|---|---|
| `hit_ratio` (`hits`/(`hits`+`misses`)) | The headline efficiency number | Low and flat with low eviction → your content simply isn't re-read; consider disabling the cache |
| `bytes_served` vs `bytes_fetched` | What the cache bought you: local bytes vs bytes pulled from the network | The number to quote when deciding if the cache earns its disk |
| `evicted_budget` + `evicted_bytes` rising while `hit_ratio` falls | Budget too small for the working set — the cache is churning | Raise `download_cache_max_bytes` |
| `evicted_pressure` > 0 | The volume ran into the 90% pressure threshold | An operations signal, not a tuning one: the disk is nearly full |
| `min_uses_rejects` | One-hit-wonders the admission filter absorbed | High values confirm the filter is earning its keep |
| `coalesced_waits` / `coalesce_timeouts` | Concurrent misses that piggybacked on another request's fill / gave up waiting | Frequent timeouts → raise `download_queue_wait_secs` or the download gate cap |
| `self_heal_drops` | Entries dropped because the on-disk file changed or vanished outside indelible's control | Should stay 0; investigate the volume if it doesn't |
| `purged` | Cached copies removed because their upload was deleted (the delete-purge invariant) | Informational — tracks delete traffic hitting cached content |

Sizing rule of thumb: budget ≥ your hot working set (for a wiki-style deployment, usually ~1–a few GB). The per-instance gauges `entries`/`bytes` in the same line show utilisation against the budget.

## Fleet deployments (read/write split)

Every instance owns its cache — there is no shared cache tier, by design (share-nothing keeps readers stateless and the hot path database-free):

- **Each reader warms its own cache from its own traffic.** Without load-balancer affinity, popular objects end up replicated on every reader. At typical working-set sizes that is the right trade — size the budget per reader, not per fleet.
- **All `download_cache_*` settings are fleet-global *values*** (readers share the writer's database) **enforced per instance** against that instance's own cache. A fleet with different disk sizes per reader uses the env override `INDELIBLE_DOWNLOAD_CACHE_MAX_BYTES` on the instances that differ — env beats the DB setting on that instance only; `0` disables that instance's cache outright.
- **Seeding is writer-local.** Uploads pass only through the writer, so `download_cache_seed_on_upload` warms the writer's cache; each reader's first read of a new object is still a cold, gated fetch. The full publish-then-read win applies to all-in-one deployments, where writer and reader are the same process.

| Dial | Where it lives | What it affects |
|---|---|---|
| `download_cache_max_bytes` | DB setting (fleet-global value) | Each instance's own budget |
| `INDELIBLE_DOWNLOAD_CACHE_MAX_BYTES` | Env / config file (per instance) | Overrides the budget on that instance only |
| `download_cache_max_object_bytes` | DB setting | Per-object ceiling, every instance |
| `download_cache_min_uses` | DB setting | Read-through admission, counted per instance |
| `download_cache_inactive_secs` | DB setting | Idle expiry, each instance's own access clock |
| `download_cache_seed_on_upload` | DB setting | Writer only (uploads run nowhere else) |

## Privacy posture

Cached entries are **plaintext bytes on the instance's disk**, held outside the audit surface of the upload store. That is why:

- **Only public uploads are cached.** Private content would put plaintext where a disk snapshot, misconfigured backup, or shared volume could expose it, and a deleted-and-shredded upload must not survive in a cache copy. (Whether a private-content opt-in should ever exist — with the fleet purge propagation it would need — is tracked separately as V2-873.)
- Cache files are owner-only (`0600`) under `<data_dir>/cache/objects`, named by content digest — the name never reveals the DataMap (which is the retrieval capability), and the digest is domain-separated from the plaintext hash.
- `download_cache_inactive_secs` doubles as the bound on how long unread cached plaintext can linger; budget `0` (or the env override `0`) drains an instance completely.
- **Deleting an upload purges its cached copy synchronously on the instance that handles the delete** (V2-824): the purge runs before the record is removed, and a purge that cannot unlink fails the delete rather than reporting a deletion while the plaintext remains readable. A download already in flight when the delete lands finishes streaming (its descriptor outlives the unlink) and any promotion it makes is taken back out. That take-back is best-effort: in the doubly-degraded case (a promotion raced the delete AND the final unlink failed) the leftover bytes are not servable — the deleted record 404s before the cache is consulted — and fall to eviction/inactivity; durable purge retry arrives with the fleet purge log (V2-873). On a reader fleet, other instances' cached copies of a deleted *public* upload are unreachable immediately (the record is gone) and age out by eviction/inactivity — acceptable for public bytes, and exactly why private content is not cached.
