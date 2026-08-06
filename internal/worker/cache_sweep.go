package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/diskusage"
	"github.com/WithAutonomi/indelible/internal/downloadcache"
	"github.com/WithAutonomi/indelible/internal/services"
)

// CacheSweepWorker enforces the download cache's disk policy (V2-823): the
// byte budget (`download_cache_max_bytes`, per-instance override via
// INDELIBLE_DOWNLOAD_CACHE_MAX_BYTES), the optional inactivity window
// (`download_cache_inactive_secs`), and — with precedence over both — the
// rule that the cache must never be the reason uploads pause: when volume
// usage approaches the disk-alert worker's 95% uploads-pause threshold, the
// sweeper sacrifices the cache toward empty. Cached bytes are pure
// acceleration, reconstructible from the network at any time; upload staging
// is durability in flight.
//
// Unlike the writer-only singleton workers gated by WorkersEnabled, this
// worker runs on EVERY role, reader replicas included: the cache is
// per-instance state under this instance's DataDir, so its hygiene must run
// wherever a cache exists. It is deliberately DB-write-free — reader
// discipline (V2-514) keeps the reader fleet clear of synchronous DB writes,
// so eviction telemetry goes to slog (stdout) only, never the system log
// table. Settings reads are fine (readers already read settings on the serve
// path).
//
// Eviction follows the nginx cache-manager shape validated in the V2-820
// research pass: bounded batches with pauses between them, LRU order from the
// store's own access clock (never filesystem atime), and transient overshoot
// between ticks accepted rather than chased. Every removal goes through
// DropGen, so a sweep decision gone stale — the entry re-promoted since the
// snapshot — degrades to a no-op instead of unlinking fresh bytes.
type CacheSweepWorker struct {
	cfg         *config.Config
	settingsSvc *services.CachedSettingsService
	store       *downloadcache.Store
	uploadSvc   *services.UploadService

	// Fleet purge propagation (V2-873): lastPurgeID is this instance's
	// high-water mark in cache_purge_log — in-memory only, because a full
	// reconciliation against uploads.cache_key subsumes log history and runs
	// at boot, every cacheReconcileInterval (the guaranteed backstop the
	// #155 panel required: it recovers purges whose log rows were pruned
	// while an unlink was stuck), and on a private-caching opt-out (which
	// must remove already-cached private plaintext, not just stop admitting
	// it). pruneLog is writer-role only (readers stay DB-write-free, V2-514).
	// privateAllowed is the last observed opt-in value, for detecting the
	// true→false transition.
	lastPurgeID    int64
	pruneLog       bool
	lastPrune      time.Time
	lastReconcile  time.Time
	privateAllowed bool

	// usage reports volume capacity for the disk-pressure trigger; injected
	// so tests can simulate a filling disk. Defaults to diskusage.Usage.
	usage func(path string) (total, free, used uint64, ok bool)
	// interval between sweeps; batch size and pause between eviction batches.
	interval time.Duration
	batch    int
	pause    time.Duration

	// lastStats is the counter snapshot as of the last emission, so the
	// periodic stats line only prints when something actually happened.
	lastStats downloadcache.MetricsSnapshot

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

const (
	// cacheSweepInterval is deliberately tighter than the disk-alert worker's
	// 5 minutes: a full cache blocks new admissions until the next sweep
	// frees headroom, so the sweep cadence bounds how long the cache can sit
	// frozen. A tick with nothing to do is two mutex-guarded map reads.
	cacheSweepInterval = time.Minute

	// cacheDiskPressurePct is where cache eviction turns aggressive: 90%
	// volume usage, comfortably before the disk-alert worker pauses uploads
	// at 95%. Between the two thresholds the sweeper is actively freeing the
	// only bytes on the volume that are safe to delete, which is the cache
	// earning its keep — durability staging wins over acceleration.
	cacheDiskPressurePct = 90.0

	// cacheSweepBatch bounds files unlinked between pauses so a large sweep
	// never monopolizes the store mutex or the disk (nginx cache-manager
	// pattern: ≤100 files per iteration).
	cacheSweepBatch = 100

	cacheSweepBatchPause = 50 * time.Millisecond

	// cachePurgeLogRetention is how long consumed-or-not purge-log rows are
	// kept before the writer prunes them. It bounds the log's size, not
	// correctness: an instance offline longer than this reconciles its whole
	// cache against uploads.cache_key at boot anyway.
	cachePurgeLogRetention = 7 * 24 * time.Hour

	// cachePurgeBatch is the per-read tail size when consuming the log.
	cachePurgeBatch = 500

	// cacheReconcileInterval is how often the full cache-vs-rows liveness
	// reconciliation re-runs after boot. It is the guaranteed erasure
	// backstop: even if a purge-log row is pruned while an instance's unlink
	// of that key is stuck, the next reconciliation re-derives the orphan
	// from uploads.cache_key and retries. Comfortably inside the 7-day log
	// retention.
	cacheReconcileInterval = 12 * time.Hour
)

// NewCacheSweepWorker creates the sweeper over the same store the download
// handler serves from.
func NewCacheSweepWorker(db *database.DB, cfg *config.Config, store *downloadcache.Store) *CacheSweepWorker {
	return &CacheSweepWorker{
		cfg:         cfg,
		settingsSvc: services.NewCachedSettingsService(services.NewSettingsService(db)),
		store:       store,
		uploadSvc:   services.NewUploadService(db),
		pruneLog:    cfg.WorkersEnabled,
		usage:       diskusage.Usage,
		interval:    cacheSweepInterval,
		batch:       cacheSweepBatch,
		pause:       cacheSweepBatchPause,
	}
}

// Start begins periodic sweeping, with one immediate sweep so a budget or
// window lowered while the instance was down is applied at boot, not a full
// tick later.
func (w *CacheSweepWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.sweep(ctx)

		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.sweep(ctx)
			}
		}
	}()

	slog.Info("download cache sweep worker started")
}

// Stop gracefully shuts down the worker.
func (w *CacheSweepWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

// sweep runs one policy pass: disk pressure first (highest precedence), then
// the inactivity window, then the byte budget — and finally the periodic
// stats emission, which runs even over an empty cache (counters may have
// moved since the last tick drained it).
func (w *CacheSweepWorker) sweep(ctx context.Context) {
	w.propagatePurges(ctx)

	if count, _ := w.store.Stats(); count > 0 {
		w.sweepDiskPressure(ctx)

		if secs := w.settingsSvc.GetIntWithBounds("download_cache_inactive_secs", 0, 0, 315360000); secs > 0 {
			w.sweepInactive(ctx, time.Now().Add(-time.Duration(secs)*time.Second))
		}

		w.sweepBudget(ctx)
	}

	w.emitStats()
}

// propagatePurges applies fleet-wide delete purges to this instance's cache
// (V2-873). A full liveness reconciliation runs at boot, every
// cacheReconcileInterval, and on a private-caching opt-out — it subsumes log
// history, so it is the guaranteed backstop for purges whose log rows were
// pruned while this instance's unlink was stuck. Steady state consumes the
// log tail: every entry's Drop is attempted (one stuck key never delays
// later deletes), but the high-water mark advances only through the
// contiguous successful prefix, so failed keys are retried from the log next
// tick (re-drops of already-purged keys are no-ops). Writer-role instances
// also prune the log hourly.
func (w *CacheSweepWorker) propagatePurges(ctx context.Context) {
	privateNow := w.settingsSvc.GetBool("download_cache_private", false)
	switch {
	case w.lastReconcile.IsZero(): // boot
	case w.privateAllowed && !privateNow: // opt-out: purge existing private plaintext
	case time.Since(w.lastReconcile) >= cacheReconcileInterval: // periodic backstop
	default:
		goto tail
	}
	if clean, err := w.reconcile(ctx, privateNow); err != nil {
		slog.Warn("download cache reconciliation failed; retrying next tick", "error", err)
		return // never consume the tail from an unreconciled baseline
	} else if clean {
		w.lastReconcile = time.Now()
	}
	// A not-clean pass (some unlink failed) completed its scan — the tail
	// may proceed — but lastReconcile stays put so the next tick re-runs the
	// reconciliation until every orphan's bytes are actually gone.

tail:
	w.privateAllowed = privateNow
	for {
		entries, err := w.uploadSvc.PurgeLogSince(w.lastPurgeID, cachePurgeBatch)
		if err != nil {
			slog.Warn("download cache purge-log read failed", "error", err)
			return
		}
		if len(entries) == 0 {
			break
		}
		stalled := false
		for _, e := range entries {
			if ctx.Err() != nil {
				return
			}
			if err := w.store.Drop(e.CacheKey); err != nil {
				if !stalled {
					slog.Warn("download cache propagated purge failed; will retry next tick",
						"key", e.CacheKey, "error", err)
				}
				stalled = true
				continue // later purges still apply this tick
			}
			if !stalled {
				w.lastPurgeID = e.ID
			}
		}
		if stalled || len(entries) < cachePurgeBatch {
			break
		}
	}

	if w.pruneLog && time.Since(w.lastPrune) >= time.Hour {
		w.lastPrune = time.Now()
		if n, err := w.uploadSvc.PrunePurgeLog(time.Now().Add(-cachePurgeLogRetention)); err != nil {
			slog.Warn("cache purge-log prune failed", "error", err)
		} else if n > 0 {
			slog.Info("cache purge-log pruned", "rows", n)
		}
	}
}

// reconcile validates every cached key against live upload rows and purges
// the orphans — deletes that happened while this instance was down or whose
// log rows are gone — plus, policy-awareness (#155 panel finding 3): private
// keys are orphans whenever download_cache_private is off, so an opt-out
// removes already-cached private plaintext instead of stranding it.
//
// The log high-water mark is read BEFORE the cache snapshot, and the
// row-delete + log-append commit atomically (UploadService.Delete), so a
// delete landing during reconciliation either logs past that mark (caught by
// the tail) or its row is already invisible (caught by the liveness check) —
// no interleaving skips it. Drops are unconditional (Store.Drop): the only
// concurrent promotion of a non-live key is a resurrection, which the
// promote-site guard is already unwinding.
//
// Returns clean=false when some unlink failed: the scan completed and the
// caller may consume the tail, but the reconciliation must re-run next tick
// until the bytes are gone.
func (w *CacheSweepWorker) reconcile(ctx context.Context, privateAllowed bool) (clean bool, err error) {
	maxID, err := w.uploadSvc.MaxPurgeLogID()
	if err != nil {
		return false, err
	}
	count, _ := w.store.Stats()
	if count == 0 {
		w.lastPurgeID = maxID
		return true, nil
	}
	victims := w.store.Oldest(count)
	keys := make([]string, len(victims))
	for i, v := range victims {
		keys[i] = v.Key
	}
	visibility, err := w.uploadSvc.CacheKeyVisibility(keys)
	if err != nil {
		return false, err
	}
	purged, stuck := 0, 0
	for _, v := range victims {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		vis, live := visibility[v.Key]
		if live && (vis == "public" || privateAllowed) {
			continue
		}
		if err := w.store.Drop(v.Key); err != nil {
			stuck++
			continue
		}
		purged++
	}
	w.lastPurgeID = maxID
	if purged > 0 || stuck > 0 {
		slog.Info("download cache reconciled", "purged", purged, "unlink_failures", stuck)
	}
	return stuck == 0, nil
}

// emitStats writes one cumulative "download cache stats" line to slog (V2-825)
// when any counter moved since the last emission — the operator's sizing
// telemetry (hit ratio, eviction churn, bytes saved) until the V2-767 traffic
// accounting gives these a fleet-aggregable home. Cumulative since boot;
// stdout-only, like everything on the reader fleet (V2-514).
func (w *CacheSweepWorker) emitStats() {
	snap := w.store.Metrics().Snapshot()
	if snap == w.lastStats {
		return
	}
	w.lastStats = snap
	count, bytes := w.store.Stats()
	ratio := 0.0
	if total := snap.Hits + snap.Misses; total > 0 {
		ratio = float64(snap.Hits) / float64(total)
	}
	slog.Info("download cache stats",
		"entries", count, "bytes", bytes,
		"hits", snap.Hits, "misses", snap.Misses, "hit_ratio", ratio,
		"bytes_served", snap.BytesServed, "bytes_fetched", snap.BytesFetch,
		"promoted_read_through", snap.PromotedReadThrough, "promoted_seeded", snap.PromotedSeeded,
		"min_uses_rejects", snap.MinUsesRejects,
		"coalesced_waits", snap.CoalescedWaits, "coalesce_timeouts", snap.CoalesceTimeouts,
		"evicted_budget", snap.EvictedBudget, "evicted_inactive", snap.EvictedInactive,
		"evicted_pressure", snap.EvictedPressure, "evicted_bytes", snap.EvictedBytes,
		"self_heal_drops", snap.SelfHealDrops, "purged", snap.Purged)
}

// sweepDiskPressure evicts toward empty while the DataDir volume sits at or
// above cacheDiskPressurePct, re-measuring between batches so eviction stops
// the moment either the pressure clears or the cache is spent. The cache's
// own budget is irrelevant here by design.
func (w *CacheSweepWorker) sweepDiskPressure(ctx context.Context) {
	evicted, freed := 0, int64(0)
	for ctx.Err() == nil {
		total, _, used, ok := w.usage(w.cfg.DataDir)
		if !ok || total == 0 {
			break // couldn't determine — leave pressure decisions to the disk-alert worker
		}
		pct := float64(used) / float64(total) * 100.0
		if pct < cacheDiskPressurePct {
			break
		}
		n, b := w.evictBatch(time.Time{})
		if n == 0 {
			if count, _ := w.store.Stats(); count > 0 {
				// Victims existed but every drop went stale — next tick retries.
				break
			}
			slog.Warn("disk pressure persists with download cache empty — nothing left to sacrifice",
				"usage_pct", pct)
			break
		}
		evicted += n
		freed += b
		w.batchPause(ctx)
	}
	if evicted > 0 {
		m := w.store.Metrics()
		m.EvictedPressure.Add(int64(evicted))
		m.EvictedBytes.Add(freed)
		slog.Warn("download cache evicted under disk pressure",
			"entries", evicted, "bytes", freed, "threshold_pct", cacheDiskPressurePct)
	}
}

// sweepInactive removes entries not accessed since cutoff — the second
// pruning axis (nginx `inactive` pattern), decoupled from the byte budget.
// Doubles as the privacy dial: it bounds how long cached plaintext can
// linger on this instance's disk without being read (V2-824).
func (w *CacheSweepWorker) sweepInactive(ctx context.Context, cutoff time.Time) {
	evicted, freed := 0, int64(0)
	for ctx.Err() == nil {
		n, b := w.evictBatch(cutoff)
		if n == 0 {
			break
		}
		evicted += n
		freed += b
		w.batchPause(ctx)
	}
	if evicted > 0 {
		m := w.store.Metrics()
		m.EvictedInactive.Add(int64(evicted))
		m.EvictedBytes.Add(freed)
		slog.Info("download cache evicted inactive entries",
			"entries", evicted, "bytes", freed, "cutoff", cutoff.Format(time.RFC3339))
	}
}

// sweepBudget evicts LRU entries until the cache fits its low-water mark: the
// budget minus admission headroom. Evicting to the budget line itself would
// freeze a full cache — admission is stop-at-full (PromoteIfFits refuses
// anything over budget), so without headroom no new object could ever enter
// once the cache filled. The headroom is the larger of 10% of the budget and
// one max-size object (clamped to half the budget for degenerate configs), so
// each tick leaves room for fresh, hotter content to displace the LRU tail.
//
// A budget of zero — the cache disabled fleet-wide or on this instance —
// drains whatever a previous configuration left behind: disabled must
// converge to empty disk, not to bytes nothing will ever serve (an index-only
// forget would strand plaintext on disk, the V2-824 concern).
func (w *CacheSweepWorker) sweepBudget(ctx context.Context) {
	budget := w.cfg.DownloadCacheBudget(
		int64(w.settingsSvc.GetIntWithBounds("download_cache_max_bytes", 0, 0, 1<<50)))
	maxObject := int64(w.settingsSvc.GetIntWithBounds("download_cache_max_object_bytes", 64<<20, 1, 1<<40))

	headroom := budget / 10
	if maxObject > headroom {
		headroom = maxObject
	}
	if headroom > budget/2 {
		headroom = budget / 2
	}
	target := budget - headroom

	evicted, freed := 0, int64(0)
	for ctx.Err() == nil {
		_, bytes := w.store.Stats()
		if bytes <= target {
			break
		}
		n, b := w.evictBatchToBytes(target)
		if n == 0 {
			break // all drops went stale, or nothing left — next tick retries
		}
		evicted += n
		freed += b
		w.batchPause(ctx)
	}
	if evicted > 0 {
		m := w.store.Metrics()
		m.EvictedBudget.Add(int64(evicted))
		m.EvictedBytes.Add(freed)
		slog.Info("download cache evicted over budget",
			"entries", evicted, "bytes", freed, "budget", budget, "target", target)
	}
}

// evictBatch drops up to one batch of LRU victims. A non-zero cutoff limits
// the batch to entries last accessed before it (the inactivity pass); a zero
// cutoff takes victims unconditionally (the disk-pressure pass). Returns how
// many entries and bytes were actually dropped — stale (key, gen) pairs count
// for nothing.
func (w *CacheSweepWorker) evictBatch(cutoff time.Time) (int, int64) {
	evicted, freed := 0, int64(0)
	for _, v := range w.store.Oldest(w.batch) {
		if !cutoff.IsZero() && !v.LastAccess.Before(cutoff) {
			break // snapshot is oldest-first: the rest are fresher still
		}
		if w.store.DropGen(v.Key, v.Gen) {
			evicted++
			freed += v.Size
		}
	}
	return evicted, freed
}

// evictBatchToBytes drops up to one batch of LRU victims, stopping early once
// the store's live byte count reaches target.
func (w *CacheSweepWorker) evictBatchToBytes(target int64) (int, int64) {
	evicted, freed := 0, int64(0)
	for _, v := range w.store.Oldest(w.batch) {
		if _, bytes := w.store.Stats(); bytes <= target {
			break
		}
		if w.store.DropGen(v.Key, v.Gen) {
			evicted++
			freed += v.Size
		}
	}
	return evicted, freed
}

// batchPause sleeps the inter-batch interval, waking early on shutdown.
func (w *CacheSweepWorker) batchPause(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(w.pause):
	}
}
