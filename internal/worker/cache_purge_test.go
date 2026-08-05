package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
// KeyForIdentifier(identifier), and returns its id and key.
func mkStoredUpload(t *testing.T, db *database.DB, id int64, uuid, identifier string) string {
	t.Helper()
	key := downloadcache.KeyForIdentifier(identifier)
	if _, err := db.Exec(`INSERT INTO uploads (id, uuid, user_id, filename, original_filename, file_size, content_type, visibility, status, data_map, cache_key)
		VALUES (?, ?, 8001, 'f.bin', 'f.bin', 4, 'text/plain', 'private', 'completed', ?, ?)`,
		id, uuid, identifier, key); err != nil {
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
	key := mkStoredUpload(t, db, 8101, "fleet-uuid-1", "dm-fleet-1")
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
	liveKey := mkStoredUpload(t, db, 8102, "fleet-uuid-2", "dm-live-2")
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
	key := mkStoredUpload(t, db, 8103, "fleet-uuid-3", "dm-stuck-3")
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
