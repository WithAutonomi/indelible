package downloadcache

import "sync/atomic"

// Metrics is the cache's per-instance counter set (V2-825): cumulative since
// boot, atomic, and passive — the store and its callers increment, and the
// sweep worker periodically snapshots them to slog. Nothing here touches the
// database (reader discipline, V2-514); when the V2-767 traffic accounting
// lands these slot into it as the fleet-aggregable source.
//
// It hangs off the Store because every party that needs to count — the serve
// path, the sweeper, upload-side seeding — already holds the store; a
// separate handle would be one more thing to thread through constructors.
//
// Reading guide for operators (details in docs/guides/download-cache.md):
// hit ratio = Hits/(Hits+Misses) is the headline efficiency number;
// BytesServed vs BytesFetched is what the cache bought you; eviction churn
// (evictions/sec, EvictedBytes) rising while the hit ratio falls means the
// budget is too small for the working set; MinUsesRejects approximates the
// one-hit-wonder rate the admission filter is absorbing.
type Metrics struct {
	// Serve path.
	Hits        atomic.Int64 // cache hits served from local disk
	Misses      atomic.Int64 // cache-eligible requests that had to fetch
	BytesServed atomic.Int64 // object bytes served from cache (full size per hit; Range serves count the whole object)
	BytesFetch  atomic.Int64 // bytes fetched from antd on the download path

	// Admission.
	PromotedReadThrough atomic.Int64 // promotions from the download miss path
	PromotedSeeded      atomic.Int64 // promotions seeded from upload temp files (V2-822)
	MinUsesRejects      atomic.Int64 // admissions deferred by the min-uses filter

	// Fill coalescing (V2-821 singleflight).
	CoalescedWaits    atomic.Int64 // followers that parked behind another request's fill
	CoalesceTimeouts  atomic.Int64 // followers that gave up waiting and fetched themselves

	// Eviction (V2-823 sweeper), split by pass — a rising pressure count is
	// an operations signal, budget/inactive counts are sizing signals.
	EvictedBudget   atomic.Int64
	EvictedInactive atomic.Int64
	EvictedPressure atomic.Int64
	EvictedBytes    atomic.Int64 // bytes freed by eviction, all passes

	// Self-healing: entries dropped because the on-disk file vanished or
	// stopped matching its recorded identity (external interference — should
	// stay at zero in a healthy deployment).
	SelfHealDrops atomic.Int64
}

// Metrics returns the store's counter set. Never nil.
func (s *Store) Metrics() *Metrics {
	return &s.metrics
}

// MetricsSnapshot is a plain-value copy of the counters, comparable with ==
// so emitters can cheaply detect "anything changed since last time".
type MetricsSnapshot struct {
	Hits, Misses, BytesServed, BytesFetch          int64
	PromotedReadThrough, PromotedSeeded            int64
	MinUsesRejects, CoalescedWaits, CoalesceTimeouts int64
	EvictedBudget, EvictedInactive, EvictedPressure  int64
	EvictedBytes, SelfHealDrops                      int64
}

// Snapshot returns a point-in-time copy of all counters.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Hits:                m.Hits.Load(),
		Misses:              m.Misses.Load(),
		BytesServed:         m.BytesServed.Load(),
		BytesFetch:          m.BytesFetch.Load(),
		PromotedReadThrough: m.PromotedReadThrough.Load(),
		PromotedSeeded:      m.PromotedSeeded.Load(),
		MinUsesRejects:      m.MinUsesRejects.Load(),
		CoalescedWaits:      m.CoalescedWaits.Load(),
		CoalesceTimeouts:    m.CoalesceTimeouts.Load(),
		EvictedBudget:       m.EvictedBudget.Load(),
		EvictedInactive:     m.EvictedInactive.Load(),
		EvictedPressure:     m.EvictedPressure.Load(),
		EvictedBytes:        m.EvictedBytes.Load(),
		SelfHealDrops:       m.SelfHealDrops.Load(),
	}
}
