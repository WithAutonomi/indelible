package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/downloadcache"
	"github.com/WithAutonomi/indelible/internal/services"
)

// sweepKey returns the i-th deterministic valid cache key. Promotion order is
// access order (each promotion stamps the clock), so tests promote in the
// order they want the LRU tail laid out.
func sweepKey(i int) string {
	return fmt.Sprintf("%064x", i+1)
}

// newSweepEnv builds a ready store with n promoted entries of size bytes
// each, plus a sweeper over it with the given runtime settings applied.
// The fake disk reports comfortable usage (10%) unless the test replaces
// w.usage.
func newSweepEnv(t *testing.T, n int, content string, settings map[string]string) (*CacheSweepWorker, *downloadcache.Store, *database.DB) {
	t.Helper()
	db := dbtest.OpenDB(t)
	settingsSvc := services.NewSettingsService(db)
	for k, v := range settings {
		if err := settingsSvc.SetInternal(k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}

	dir := t.TempDir()
	store := downloadcache.New(filepath.Join(dir, "objects"))
	if err := store.Scan(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}
	for i := 0; i < n; i++ {
		src := filepath.Join(dir, fmt.Sprintf("tmp-%d", i))
		if err := os.WriteFile(src, []byte(content), 0600); err != nil {
			t.Fatalf("write temp %d: %v", i, err)
		}
		if _, err := store.PromoteIfFits(sweepKey(i), src, 1<<40); err != nil {
			t.Fatalf("promote %d: %v", i, err)
		}
	}

	cfg := &config.Config{DataDir: dir}
	w := NewCacheSweepWorker(db, cfg, store)
	w.pause = time.Millisecond
	w.usage = func(string) (total, free, used uint64, ok bool) {
		return 1000, 900, 100, true // 10% used — no pressure
	}
	return w, store, db
}

func TestCacheSweep_BudgetEvictsLRUTailToLowWater(t *testing.T) {
	// 10 entries × 100 bytes = 1000 = the budget. Max-object 10 makes the
	// headroom rule pick 10% of budget (100), so the sweep must evict exactly
	// the single least-recently-used entry to reach the 900-byte target.
	w, store, _ := newSweepEnv(t, 10, string(make([]byte, 100)), map[string]string{
		"download_cache_max_bytes":        "1000",
		"download_cache_max_object_bytes": "10",
	})

	// Freshen the oldest-promoted entry: the LRU tail is now entry 1, and
	// eviction respecting the access clock (not insertion order) must take it.
	if _, ok := store.Get(sweepKey(0)); !ok {
		t.Fatal("warm-up hit missed")
	}

	w.sweep(context.Background())

	if count, bytes := store.Stats(); count != 9 || bytes != 900 {
		t.Fatalf("after sweep: (%d entries, %d bytes), want (9, 900)", count, bytes)
	}
	if _, ok := store.Get(sweepKey(1)); ok {
		t.Fatal("LRU entry survived a sweep that had to evict it")
	}
	if _, ok := store.Get(sweepKey(0)); !ok {
		t.Fatal("freshly accessed entry was evicted despite being MRU")
	}
}

func TestCacheSweep_UnderBudgetIsUntouched(t *testing.T) {
	w, store, _ := newSweepEnv(t, 3, "abc", map[string]string{
		"download_cache_max_bytes":        "1000000",
		"download_cache_max_object_bytes": "10",
	})

	w.sweep(context.Background())

	if count, bytes := store.Stats(); count != 3 || bytes != 9 {
		t.Fatalf("after sweep: (%d, %d), want the cache untouched at (3, 9)", count, bytes)
	}
}

func TestCacheSweep_ZeroBudgetDrainsLeftovers(t *testing.T) {
	// download_cache_max_bytes unset = 0 = disabled: a cache left behind by a
	// previous configuration must converge to empty disk, not linger as
	// unservable plaintext.
	w, store, _ := newSweepEnv(t, 5, "leftover", nil)

	w.sweep(context.Background())

	if count, bytes := store.Stats(); count != 0 || bytes != 0 {
		t.Fatalf("after sweep: (%d, %d), want a disabled cache fully drained", count, bytes)
	}
}

func TestCacheSweep_InstanceOverrideBeatsFleetBudget(t *testing.T) {
	// Fleet-global budget says 1MB; this instance is pinned to 0 via the boot
	// config override. The override must rule: drain.
	w, store, _ := newSweepEnv(t, 4, "bytes", map[string]string{
		"download_cache_max_bytes": "1000000",
	})
	var zero int64
	w.cfg.DownloadCacheMaxBytes = &zero

	w.sweep(context.Background())

	if count, _ := store.Stats(); count != 0 {
		t.Fatalf("%d entries survived an instance pinned to budget 0", count)
	}
}

func TestCacheSweep_InactiveWindowPrunesByAccessClock(t *testing.T) {
	w, store, _ := newSweepEnv(t, 3, "stale?", map[string]string{
		"download_cache_max_bytes":     "1000000",
		"download_cache_inactive_secs": "1",
	})

	// Let every entry age past the 1s window, then touch one: only it has
	// been accessed inside the window and only it may survive.
	time.Sleep(1100 * time.Millisecond)
	if _, ok := store.Get(sweepKey(2)); !ok {
		t.Fatal("warm-up hit missed")
	}

	w.sweep(context.Background())

	if count, _ := store.Stats(); count != 1 {
		t.Fatalf("%d entries after inactive sweep, want exactly the touched one", count)
	}
	if _, ok := store.Get(sweepKey(2)); !ok {
		t.Fatal("recently accessed entry pruned by the inactivity window")
	}
}

func TestCacheSweep_DiskPressureSacrificesCacheDespiteBudget(t *testing.T) {
	// A generous budget and no inactivity window — but the volume is at 92%.
	// Uploads-pause precedence: the sweeper must evict toward empty anyway,
	// and stop the moment the pressure clears. The fake disk credits evicted
	// cache bytes back as free space: pressure clears after two of the four
	// 100-byte entries go.
	w, store, _ := newSweepEnv(t, 4, string(make([]byte, 100)), map[string]string{
		"download_cache_max_bytes": "1000000",
	})
	w.batch = 1 // per-batch re-measure at single-entry granularity for the assertion
	w.usage = func(string) (total, free, used uint64, ok bool) {
		_, bytes := store.Stats()
		// 91.0% with the cache full (400 bytes); dips under the 90% threshold
		// once two entries have been freed (89.9% at 200 bytes).
		used = 8700 + uint64(bytes)
		return 10000, 10000 - used, used, true
	}

	w.sweep(context.Background())

	if count, bytes := store.Stats(); count != 2 || bytes != 200 {
		t.Fatalf("after pressure sweep: (%d, %d), want (2, 200) — evict until under 90%%, not to empty", count, bytes)
	}
}

func TestCacheSweep_DiskPressureCanEmptyTheCache(t *testing.T) {
	// Pressure that never clears: the cache has nothing left to give and the
	// sweeper must end at empty without spinning.
	w, store, _ := newSweepEnv(t, 3, "sacrifice", map[string]string{
		"download_cache_max_bytes": "1000000",
	})
	w.usage = func(string) (total, free, used uint64, ok bool) {
		return 1000, 20, 980, true // 98%, regardless of what we evict
	}

	done := make(chan struct{})
	go func() {
		w.sweep(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("sweep did not terminate under persistent disk pressure")
	}

	if count, _ := store.Stats(); count != 0 {
		t.Fatalf("%d entries survived persistent disk pressure", count)
	}
}

func TestCacheSweep_StartStopLifecycle(t *testing.T) {
	w, _, _ := newSweepEnv(t, 1, "x", nil)
	w.interval = 10 * time.Millisecond
	w.Start()
	time.Sleep(50 * time.Millisecond)
	w.Stop() // must not hang or race (-race build)
}

// logCapture is a minimal slog.Handler that records message names, so tests
// can assert on emission without parsing formatted output.
type logCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (h *logCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *logCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, r.Message)
	return nil
}
func (h *logCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logCapture) WithGroup(string) slog.Handler      { return h }

func (h *logCapture) count(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

func TestCacheSweep_EvictionCounters(t *testing.T) {
	w, store, _ := newSweepEnv(t, 10, string(make([]byte, 100)), map[string]string{
		"download_cache_max_bytes":        "1000",
		"download_cache_max_object_bytes": "10",
	})

	w.sweep(context.Background())

	m := store.Metrics().Snapshot()
	if m.EvictedBudget != 1 || m.EvictedBytes != 100 {
		t.Fatalf("evicted budget/bytes = %d/%d, want 1/100", m.EvictedBudget, m.EvictedBytes)
	}
	if m.EvictedInactive != 0 || m.EvictedPressure != 0 {
		t.Fatalf("inactive/pressure counters moved (%d/%d) on a budget-only sweep", m.EvictedInactive, m.EvictedPressure)
	}
}

func TestCacheSweep_StatsEmissionOnlyOnChange(t *testing.T) {
	capt := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capt))
	defer slog.SetDefault(prev)

	w, store, _ := newSweepEnv(t, 0, "", map[string]string{
		"download_cache_max_bytes": "1000000",
	})

	// Nothing has happened: no stats line.
	w.sweep(context.Background())
	if n := capt.count("download cache stats"); n != 0 {
		t.Fatalf("stats emitted %d times on a quiet cache, want 0", n)
	}

	// A counter moved: exactly one line; an unchanged follow-up tick stays quiet.
	store.Metrics().Hits.Add(1)
	w.sweep(context.Background())
	w.sweep(context.Background())
	if n := capt.count("download cache stats"); n != 1 {
		t.Fatalf("stats emitted %d times after one change, want exactly 1", n)
	}
}

// A cache directory the process cannot unlink from must not wedge or spin the
// sweeper, and must not corrupt accounting: the failed drops make no progress,
// the pass breaks, and the next tick retries.
func TestCacheSweep_UnlinkFailureMakesNoFalseProgress(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	w, store, _ := newSweepEnv(t, 3, "immovable", nil) // budget 0 = drain everything
	locked := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		p, ok := store.Get(sweepKey(i))
		if !ok {
			t.Fatalf("entry %d missing", i)
		}
		dir := filepath.Dir(p)
		if err := os.Chmod(dir, 0500); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		locked = append(locked, dir)
	}
	t.Cleanup(func() {
		for _, d := range locked {
			_ = os.Chmod(d, 0700)
		}
	})

	done := make(chan struct{})
	go func() {
		w.sweep(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("sweep hung on unremovable cache files")
	}

	if count, bytes := store.Stats(); count != 3 || bytes != int64(3*len("immovable")) {
		t.Fatalf("accounting changed despite zero files unlinked: (%d, %d)", count, bytes)
	}
}
