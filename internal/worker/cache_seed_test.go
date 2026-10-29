package worker

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/downloadcache"
	"github.com/WithAutonomi/indelible/internal/services"
)

const seedContentID = "a-published-datamap-address"

// newSeedEnv builds an upload worker over a ready store, with the given
// runtime settings applied and a staged temp file of `content` bytes.
func newSeedEnv(t *testing.T, content string, settings map[string]string) (*UploadWorker, *downloadcache.Store, *services.Upload) {
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

	temp := filepath.Join(dir, "upload-staged")
	if err := os.WriteFile(temp, []byte(content), 0600); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	cfg := &config.Config{
		DataDir:             dir,
		WalletEncryptionKey: "1111111111111111111111111111111111111111111111111111111111111111",
	}
	w := NewUploadWorker(db, cfg, store)

	// A real uploads row backs the in-memory struct: the V2-824 resurrection
	// guard re-verifies the row after promoting, so a rowless upload would
	// read as "deleted mid-seed" and the seed would be purged.
	if _, err := db.Exec(`INSERT INTO users (id, email) VALUES (7001, 'seed-test@example.com')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO uploads (id, uuid, user_id, filename, original_filename, file_size, content_type, visibility, status, datamap_address)
		VALUES (7101, 'seed-test-uuid', 7001, 'seed.txt', 'seed.txt', ?, 'text/plain', 'public', 'completed', ?)`,
		len(content), seedContentID); err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	upload := &services.Upload{
		ID:         7101,
		UUID:       "seed-test-uuid",
		Visibility: "public",
		TempPath:   sql.NullString{String: temp, Valid: true},
	}
	return w, store, upload
}

// Resurrection guard (V2-824): deletion is allowed the moment the row flips
// to completed, and the seed promotes just after — a seed for a row that is
// already gone must be taken back out, not left as unpurgeable plaintext.
func TestSeedDownloadCache_PurgedWhenRowDeleted(t *testing.T) {
	w, store, upload := newSeedEnv(t, "deleted before seed landed", map[string]string{
		"download_cache_max_bytes": "1000000",
	})
	if err := w.uploadSvc.Delete(upload.ID); err != nil {
		t.Fatalf("delete row: %v", err)
	}

	w.seedDownloadCache(upload, seedContentID)

	if count, _ := store.Stats(); count != 0 {
		t.Fatalf("seed outlived its upload row: %d entries", count)
	}
	if _, ok := store.Get(downloadcache.KeyForIdentifier(seedContentID)); ok {
		t.Fatal("purged seed still hits")
	}
	if got := store.Metrics().Purged.Load(); got != 1 {
		t.Fatalf("Purged = %d, want 1", got)
	}
}

func TestSeedDownloadCache_PromotesStagedBytes(t *testing.T) {
	w, store, upload := newSeedEnv(t, "published page bytes", map[string]string{
		"download_cache_max_bytes": "1000000",
	})

	w.seedDownloadCache(upload, seedContentID)

	// The temp file was renamed, not copied — cleanupTempFile then no-ops.
	if _, err := os.Lstat(upload.TempPath.String); !os.IsNotExist(err) {
		t.Fatal("seeding must move the staged file into the cache")
	}
	p, ok := store.Get(downloadcache.KeyForIdentifier(seedContentID))
	if !ok {
		t.Fatal("seeded entry missed under the shared content key")
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "published page bytes" {
		t.Fatalf("seeded content = %q, %v", b, err)
	}
}

func TestSeedDownloadCache_SkipsMinUsesDeliberately(t *testing.T) {
	// min_uses gates read-through admission; a seed is the publish-then-read
	// bet and must promote on the spot even with a high warm-up bar.
	w, store, upload := newSeedEnv(t, "hot off the press", map[string]string{
		"download_cache_max_bytes": "1000000",
		"download_cache_min_uses":  "50",
	})

	w.seedDownloadCache(upload, seedContentID)

	if _, ok := store.Get(downloadcache.KeyForIdentifier(seedContentID)); !ok {
		t.Fatal("seed was blocked by the min-uses read-through filter")
	}
}

func TestSeedDownloadCache_RespectsSeedSwitch(t *testing.T) {
	w, store, upload := newSeedEnv(t, "archive bytes", map[string]string{
		"download_cache_max_bytes":      "1000000",
		"download_cache_seed_on_upload": "false",
	})

	w.seedDownloadCache(upload, seedContentID)

	if count, _ := store.Stats(); count != 0 {
		t.Fatal("seeded despite download_cache_seed_on_upload=false")
	}
	if _, err := os.Lstat(upload.TempPath.String); err != nil {
		t.Fatal("temp file must be left for normal cleanup when seeding is off")
	}
}

func TestSeedDownloadCache_DisabledCacheNeverSeeds(t *testing.T) {
	// download_cache_max_bytes unset (0 = cache off) — seeding must not
	// resurrect a disabled cache.
	w, store, upload := newSeedEnv(t, "bytes", nil)

	w.seedDownloadCache(upload, seedContentID)

	if count, _ := store.Stats(); count != 0 {
		t.Fatal("seeded into a disabled cache")
	}
}

func TestSeedDownloadCache_InstanceOverrideBlocksSeeding(t *testing.T) {
	// Fleet budget on, this instance pinned to 0 — the writer respects its
	// own override exactly like the serve path and sweeper do.
	w, store, upload := newSeedEnv(t, "bytes", map[string]string{
		"download_cache_max_bytes": "1000000",
	})
	var zero int64
	w.cfg.DownloadCacheMaxBytes = &zero

	w.seedDownloadCache(upload, seedContentID)

	if count, _ := store.Stats(); count != 0 {
		t.Fatal("seeded despite a per-instance budget override of 0")
	}
}

func TestSeedDownloadCache_RespectsObjectCeiling(t *testing.T) {
	w, store, upload := newSeedEnv(t, "definitely more than ten bytes", map[string]string{
		"download_cache_max_bytes":        "1000000",
		"download_cache_max_object_bytes": "10",
	})

	w.seedDownloadCache(upload, seedContentID)

	if count, _ := store.Stats(); count != 0 {
		t.Fatal("seeded an object over the per-object ceiling")
	}
	if _, err := os.Lstat(upload.TempPath.String); err != nil {
		t.Fatal("oversized temp file must survive for normal cleanup")
	}
}

func TestSeedDownloadCache_ToleratesNilStoreAndEmptyID(t *testing.T) {
	w, _, upload := newSeedEnv(t, "bytes", map[string]string{
		"download_cache_max_bytes": "1000000",
	})

	w.seedDownloadCache(upload, "") // no identifier — no-op, no panic

	w.dlCache = nil
	w.seedDownloadCache(upload, seedContentID) // nil store — no-op, no panic

	if _, err := os.Lstat(upload.TempPath.String); err != nil {
		t.Fatal("temp file must be untouched by no-op seeds")
	}
}

func TestSeedDownloadCache_OverBudgetIsNotFatal(t *testing.T) {
	// Budget smaller than the object: the promote is refused and nothing
	// breaks — the upload path treats seeding as pure opportunism.
	w, store, upload := newSeedEnv(t, "these bytes exceed the tiny budget", map[string]string{
		"download_cache_max_bytes": "10",
	})

	w.seedDownloadCache(upload, seedContentID)

	if count, _ := store.Stats(); count != 0 {
		t.Fatal("promoted over budget")
	}
	if _, err := os.Lstat(upload.TempPath.String); err != nil {
		t.Fatal("refused seed must leave the temp file in place")
	}
}
