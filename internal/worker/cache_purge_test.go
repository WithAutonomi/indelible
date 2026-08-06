package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/downloadcache"
	"github.com/WithAutonomi/indelible/internal/services"
)

// V2-873 fleet purge propagation: the sweep worker of a *remote* instance
// (any instance other than the one that handled the delete) must apply
// deletes via the purge log within one tick, and reconcile its whole cache
// against live rows at boot.

// newPurgeEnv builds a db, a ready store standing in for a remote reader's
// cache, and a sweep worker over it (workers enabled so prune paths run).
func newPurgeEnv(t *testing.T) (*database.DB, *downloadcache.Store, *CacheSweepWorker, *services.UploadService) {
	t.Helper()
	db := dbtest.OpenDB(t)
	dir := t.TempDir()
	store := downloadcache.New(filepath.Join(dir, "objects"))
	if err := store.Scan(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}
	cfg := &config.Config{DataDir: dir, WorkersEnabled: true}
	w := NewCacheSweepWorker(db, cfg, store)
	if _, err := db.Exec(`INSERT INTO users (id, email) VALUES (8001, 'purge-test@example.com')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db, store, w, services.NewUploadService(db)
}

// mkStoredUpload inserts a completed upload row whose cache_key matches
// KeyForIdentifier(identifier), and returns its key.
func mkStoredUpload(t *testing.T, db *database.DB, id int64, uuid, identifier, visibility string) string {
	t.Helper()
	key := downloadcache.KeyForIdentifier(identifier)
	if _, err := db.Exec(`INSERT INTO uploads (id, uuid, user_id, filename, original_filename, file_size, content_type, visibility, status, data_map, cache_key)
		VALUES (?, ?, 8001, 'f.bin', 'f.bin', 4, 'text/plain', ?, 'completed', ?, ?)`,
		id, uuid, visibility, identifier, key); err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	return key
}

// cacheEntry promotes content into the store under key and returns its path.
func cacheEntry(t *testing.T, store *downloadcache.Store, dir, key, content string) string {
	t.Helper()
	temp := filepath.Join(dir, "stage-"+key[:8])
	if err := os.WriteFile(temp, []byte(content), 0600); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := store.PromoteIfFits(key, temp, 1<<30); err != nil {
		t.Fatalf("promote: %v", err)
	}
	p, ok := store.Get(key)
	if !ok {
		t.Fatal("promoted entry missed")
	}
	return p
}

func TestPropagatePurges_RemoteInstanceAppliesDelete(t *testing.T) {
	db, store, w, svc := newPurgeEnv(t)
	dir := t.TempDir()
	key := mkStoredUpload(t, db, 8101, "fleet-uuid-1", "dm-fleet-1", "public")
	p := cacheEntry(t, store, dir, key, "fleet bytes")

	// Boot tick: the row is live, so reconciliation keeps the entry.
	w.propagatePurges(context.Background())
	if _, ok := store.Get(key); !ok {
		t.Fatal("boot reconciliation purged a live entry")
	}

	// The delete lands on some other instance: service-level delete appends
	// to the purge log; this instance's next tick must apply it.
	if err := svc.Delete(8101); err != nil {
		t.Fatalf("delete: %v", err)
	}
	w.propagatePurges(context.Background())

	if _, ok := store.Get(key); ok {
		t.Fatal("remote entry survived the propagated purge")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("remote plaintext still on disk after propagated purge")
	}
}

func TestBootReconcile_DropsOrphansKeepsLive(t *testing.T) {
	db, store, w, _ := newPurgeEnv(t)
	dir := t.TempDir()
	liveKey := mkStoredUpload(t, db, 8102, "fleet-uuid-2", "dm-live-2", "public")
	cacheEntry(t, store, dir, liveKey, "live bytes")
	orphanKey := downloadcache.KeyForIdentifier("dm-orphan-2")
	orphanPath := cacheEntry(t, store, dir, orphanKey, "orphan bytes")

	w.propagatePurges(context.Background())

	if _, ok := store.Get(liveKey); !ok {
		t.Fatal("reconciliation dropped an entry with a live row")
	}
	if _, ok := store.Get(orphanKey); ok {
		t.Fatal("reconciliation kept an orphan")
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatal("orphan bytes still on disk")
	}
}

func TestPropagatePurges_FailedUnlinkHaltsHighWater(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	db, store, w, svc := newPurgeEnv(t)
	dir := t.TempDir()
	key := mkStoredUpload(t, db, 8103, "fleet-uuid-3", "dm-stuck-3", "public")
	p := cacheEntry(t, store, dir, key, "stuck bytes")

	w.propagatePurges(context.Background()) // boot with the row live

	if err := svc.Delete(8103); err != nil {
		t.Fatalf("delete: %v", err)
	}
	parent := filepath.Dir(p)
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })

	before := w.lastPurgeID
	w.propagatePurges(context.Background())
	if w.lastPurgeID != before {
		t.Fatal("high-water mark advanced past a failed unlink")
	}
	if _, ok := store.Get(key); !ok {
		t.Fatal("entry must stay indexed while its bytes are stuck")
	}

	// Obstruction cleared: the next tick retries and completes the purge.
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	w.propagatePurges(context.Background())
	if _, ok := store.Get(key); ok {
		t.Fatal("retry tick did not purge")
	}
	if w.lastPurgeID == before {
		t.Fatal("high-water mark must advance after the retry succeeds")
	}
}

func TestPropagatePurges_WriterPrunesOldRows(t *testing.T) {
	db, _, w, svc := newPurgeEnv(t)

	if _, err := db.Exec(`INSERT INTO cache_purge_log (cache_key, deleted_at) VALUES ('ancient', '2020-01-01 00:00:00')`); err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	w.propagatePurges(context.Background())

	entries, err := svc.PurgeLogSince(0, 10)
	if err != nil {
		t.Fatalf("PurgeLogSince: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("old purge-log rows survived the writer prune: %+v", entries)
	}
}

// #155 panel finding 2, starvation half: one stuck key must not delay later
// deletes — every entry's Drop is attempted per tick even while the
// high-water mark stalls at the failed key.
func TestPropagatePurges_FailedKeyDoesNotDelayLaterPurges(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	db, store, w, svc := newPurgeEnv(t)
	dir := t.TempDir()
	stuckKey := mkStoredUpload(t, db, 8104, "fleet-uuid-4", "dm-stuck-4", "public")
	stuckPath := cacheEntry(t, store, dir, stuckKey, "stuck bytes")
	laterKey := mkStoredUpload(t, db, 8105, "fleet-uuid-5", "dm-later-5", "public")
	laterPath := cacheEntry(t, store, dir, laterKey, "later bytes")
	if filepath.Dir(stuckPath) == filepath.Dir(laterPath) {
		t.Fatal("fixture keys share a fanout dir; pick different identifiers")
	}

	w.propagatePurges(context.Background()) // boot, both rows live

	if err := svc.Delete(8104); err != nil {
		t.Fatalf("delete stuck: %v", err)
	}
	if err := svc.Delete(8105); err != nil {
		t.Fatalf("delete later: %v", err)
	}
	parent := filepath.Dir(stuckPath)
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })

	before := w.lastPurgeID
	w.propagatePurges(context.Background())
	if _, ok := store.Get(laterKey); ok {
		t.Fatal("a later delete was delayed behind the stuck key")
	}
	if _, ok := store.Get(stuckKey); !ok {
		t.Fatal("stuck entry must stay indexed while its bytes are stuck")
	}
	if w.lastPurgeID != before {
		t.Fatal("high-water mark advanced past the failed key")
	}

	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	w.propagatePurges(context.Background())
	if _, ok := store.Get(stuckKey); ok {
		t.Fatal("retry tick did not purge the stuck key")
	}
}

// #155 panel finding 2, prune-loss half — the panel's exact reproduction:
// a stuck unlink stalls the watermark, the writer prunes the only retry
// record, permissions recover... and the tail has nothing to retry. The
// periodic full reconciliation is the guaranteed backstop that recovers it.
func TestPropagatePurges_PruneLossRecoveredByReconciliation(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	db, store, w, svc := newPurgeEnv(t)
	dir := t.TempDir()
	key := mkStoredUpload(t, db, 8106, "fleet-uuid-6", "dm-pruned-6", "public")
	p := cacheEntry(t, store, dir, key, "pruned-retry bytes")

	w.propagatePurges(context.Background()) // boot, row live

	if err := svc.Delete(8106); err != nil {
		t.Fatalf("delete: %v", err)
	}
	parent := filepath.Dir(p)
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
	w.propagatePurges(context.Background()) // unlink fails, watermark stalls

	// The retention prune erases the retry record while the key is stuck.
	if _, err := db.Exec(`DELETE FROM cache_purge_log`); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}

	// Tail-only tick: nothing left in the log — the orphan survives. This is
	// the loss the panel reproduced.
	w.propagatePurges(context.Background())
	if _, ok := store.Get(key); !ok {
		t.Fatal("precondition: orphan should still be cached after the log was pruned")
	}

	// The periodic reconciliation re-derives the orphan from live rows.
	w.lastReconcile = time.Now().Add(-cacheReconcileInterval - time.Minute)
	w.propagatePurges(context.Background())
	if _, ok := store.Get(key); ok {
		t.Fatal("reconciliation backstop did not purge the orphan")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("orphan bytes still on disk after reconciliation")
	}
}

// #155 panel finding 3, online half: flipping download_cache_private off
// purges already-cached private plaintext on the next tick, keeps public
// entries, and no-op ticks afterwards do no spurious work.
func TestPropagatePurges_PrivateOptOutPurgesExisting(t *testing.T) {
	db, store, w, _ := newPurgeEnv(t)
	settings := services.NewSettingsService(db)
	if err := settings.SetInternal("download_cache_private", "true"); err != nil {
		t.Fatalf("set: %v", err)
	}
	dir := t.TempDir()
	privKey := mkStoredUpload(t, db, 8107, "fleet-uuid-7", "dm-priv-7", "private")
	privPath := cacheEntry(t, store, dir, privKey, "private bytes")
	pubKey := mkStoredUpload(t, db, 8108, "fleet-uuid-8", "dm-pub-8", "public")
	cacheEntry(t, store, dir, pubKey, "public bytes")

	w.propagatePurges(context.Background()) // boot with the opt-in on: both kept
	if _, ok := store.Get(privKey); !ok {
		t.Fatal("boot purged an allowed private entry")
	}

	// No-op true→true tick: nothing purged, no transition work.
	purgedAtBoot := store.Metrics().Purged.Load()
	w.propagatePurges(context.Background())
	if got := store.Metrics().Purged.Load(); got != purgedAtBoot {
		t.Fatalf("true→true tick purged %d entries", got-purgedAtBoot)
	}

	if err := settings.SetInternal("download_cache_private", "false"); err != nil {
		t.Fatalf("flip: %v", err)
	}
	w.settingsSvc.InvalidateAll()
	w.propagatePurges(context.Background()) // transition tick

	if _, ok := store.Get(privKey); ok {
		t.Fatal("opt-out left private plaintext cached")
	}
	if _, err := os.Stat(privPath); !os.IsNotExist(err) {
		t.Fatal("private bytes still on disk after opt-out")
	}
	if _, ok := store.Get(pubKey); !ok {
		t.Fatal("opt-out must not touch public entries")
	}

	purged := store.Metrics().Purged.Load()
	w.propagatePurges(context.Background()) // no-op tick
	if got := store.Metrics().Purged.Load(); got != purged {
		t.Fatalf("no-op tick purged %d more entries", got-purged)
	}
}

// #155 panel finding 3, offline half: an instance that was down past log
// retention boots with the opt-in off — reconciliation purges its private
// entries even though their rows are live.
func TestPropagatePurges_BootPurgesPrivateWhenOptedOut(t *testing.T) {
	db, store, w, _ := newPurgeEnv(t) // download_cache_private defaults off
	dir := t.TempDir()
	privKey := mkStoredUpload(t, db, 8109, "fleet-uuid-9", "dm-priv-9", "private")
	privPath := cacheEntry(t, store, dir, privKey, "stranded private bytes")
	pubKey := mkStoredUpload(t, db, 8110, "fleet-uuid-10", "dm-pub-10", "public")
	cacheEntry(t, store, dir, pubKey, "public bytes")

	w.propagatePurges(context.Background())

	if _, ok := store.Get(privKey); ok {
		t.Fatal("boot kept private plaintext despite the opt-in being off")
	}
	if _, err := os.Stat(privPath); !os.IsNotExist(err) {
		t.Fatal("private bytes still on disk after policy-aware boot")
	}
	if _, ok := store.Get(pubKey); !ok {
		t.Fatal("policy-aware boot must keep live public entries")
	}
}
