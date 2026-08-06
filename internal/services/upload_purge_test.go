package services

import (
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/downloadcache"
)

// V2-873: cache_key stamping, purge-log fan-out, and the legacy backfill.

func cacheKeyOf(t *testing.T, svc *UploadService, id int64) string {
	t.Helper()
	var key *string
	if err := svc.db.QueryRow(`SELECT cache_key FROM uploads WHERE id = ?`, id).Scan(&key); err != nil {
		t.Fatalf("read cache_key: %v", err)
	}
	if key == nil {
		return ""
	}
	return *key
}

func TestMarkTerminalStatusesStampCacheKey(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "stamp@example.com", "S", "T")
	svc := NewUploadService(db)

	private := createTestUpload(t, svc, user.ID, "private.bin", 10)
	if err := svc.MarkCompleted(private.ID, "dm-hex-private", "0"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if got, want := cacheKeyOf(t, svc, private.ID), downloadcache.KeyForIdentifier("dm-hex-private"); got != want {
		t.Fatalf("private cache_key = %q, want %q", got, want)
	}

	public := createTestUpload(t, svc, user.ID, "public.bin", 10)
	if err := svc.MarkCompletedPublic(public.ID, "addr-public", "0"); err != nil {
		t.Fatalf("MarkCompletedPublic: %v", err)
	}
	if got, want := cacheKeyOf(t, svc, public.ID), downloadcache.KeyForIdentifier("addr-public"); got != want {
		t.Fatalf("public cache_key = %q, want %q", got, want)
	}
}

func TestDeleteAppendsPurgeLogBothDerivations(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "purgelog@example.com", "P", "L")
	svc := NewUploadService(db)

	// A published-formerly-private row carries BOTH identifiers — the purge
	// fan-out must log both derivations.
	u := createTestUpload(t, svc, user.ID, "both.bin", 10)
	if err := svc.MarkCompleted(u.ID, "dm-both", "0"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if err := svc.MarkPublished(u.ID, "addr-both"); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	if err := svc.Delete(u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	entries, err := svc.PurgeLogSince(0, 10)
	if err != nil {
		t.Fatalf("PurgeLogSince: %v", err)
	}
	want := map[string]bool{
		downloadcache.KeyForIdentifier("dm-both"):   true,
		downloadcache.KeyForIdentifier("addr-both"): true,
	}
	if len(entries) != 2 || !want[entries[0].CacheKey] || !want[entries[1].CacheKey] || entries[0].CacheKey == entries[1].CacheKey {
		t.Fatalf("purge log = %+v, want both derivations exactly once", entries)
	}

	if max, err := svc.MaxPurgeLogID(); err != nil || max != entries[1].ID {
		t.Fatalf("MaxPurgeLogID = %d (err=%v), want %d", max, err, entries[1].ID)
	}
}

func TestBackfillCacheKeysStampsLegacyRows(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "backfill@example.com", "B", "F")
	svc := NewUploadService(db)

	// Simulate pre-013 rows: terminal status + identifiers but NULL cache_key.
	private := createTestUpload(t, svc, user.ID, "legacy-private.bin", 10)
	public := createTestUpload(t, svc, user.ID, "legacy-public.bin", 10)
	if _, err := db.Exec(`UPDATE uploads SET status='completed', data_map='legacy-dm', cache_key=NULL WHERE id = ?`, private.ID); err != nil {
		t.Fatalf("legacy private: %v", err)
	}
	if _, err := db.Exec(`UPDATE uploads SET status='completed', visibility='public', datamap_address='legacy-addr', cache_key=NULL WHERE id = ?`, public.ID); err != nil {
		t.Fatalf("legacy public: %v", err)
	}

	n, err := svc.BackfillCacheKeys()
	if err != nil || n != 2 {
		t.Fatalf("BackfillCacheKeys = %d, %v; want 2, nil", n, err)
	}
	if got, want := cacheKeyOf(t, svc, private.ID), downloadcache.KeyForIdentifier("legacy-dm"); got != want {
		t.Fatalf("backfilled private key = %q, want %q", got, want)
	}
	if got, want := cacheKeyOf(t, svc, public.ID), downloadcache.KeyForIdentifier("legacy-addr"); got != want {
		t.Fatalf("backfilled public key = %q, want %q", got, want)
	}

	// Idempotent: nothing left to stamp.
	if n, err := svc.BackfillCacheKeys(); err != nil || n != 0 {
		t.Fatalf("second backfill = %d, %v; want 0, nil", n, err)
	}
}

func TestPrunePurgeLogAndLiveCacheKeys(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "prune@example.com", "P", "R")
	svc := NewUploadService(db)

	u := createTestUpload(t, svc, user.ID, "live.bin", 10)
	if err := svc.MarkCompleted(u.ID, "dm-live", "0"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	liveKey := downloadcache.KeyForIdentifier("dm-live")

	live, err := svc.CacheKeyVisibility([]string{liveKey, "0000000000000000000000000000000000000000000000000000000000000000"})
	if err != nil {
		t.Fatalf("CacheKeyVisibility: %v", err)
	}
	if live[liveKey] != "private" || len(live) != 1 {
		t.Fatalf("CacheKeyVisibility = %v, want only the live key mapped to private", live)
	}

	if _, err := db.Exec(`INSERT INTO cache_purge_log (cache_key, deleted_at) VALUES (?, ?)`, "old-key", "2020-01-01 00:00:00"); err != nil {
		t.Fatalf("insert old log row: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO cache_purge_log (cache_key) VALUES (?)`, "fresh-key"); err != nil {
		t.Fatalf("insert fresh log row: %v", err)
	}
	n, err := svc.PrunePurgeLog(time.Now().Add(-24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("PrunePurgeLog = %d, %v; want 1, nil", n, err)
	}
	entries, err := svc.PurgeLogSince(0, 10)
	if err != nil || len(entries) != 1 || entries[0].CacheKey != "fresh-key" {
		t.Fatalf("post-prune log = %+v (err=%v), want only fresh-key", entries, err)
	}
}

// #155 panel finding 1: the purge-log append and the row delete are one
// transaction — a refused delete must leave no log rows behind (and other
// connections can never observe row-live + log-present).
func TestDeleteRefusedRollsBackPurgeLog(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "atomic@example.com", "A", "T")
	svc := NewUploadService(db)

	u := createTestUpload(t, svc, user.ID, "queued.bin", 10) // status stays "queued"
	if _, err := db.Exec(`UPDATE uploads SET data_map='dm-atomic' WHERE id = ?`, u.ID); err != nil {
		t.Fatalf("set identifier: %v", err)
	}

	if err := svc.Delete(u.ID); err == nil {
		t.Fatal("delete of a queued upload must be refused")
	}
	entries, err := svc.PurgeLogSince(0, 10)
	if err != nil || len(entries) != 0 {
		t.Fatalf("refused delete leaked purge-log rows: %+v (err=%v)", entries, err)
	}
	if _, err := svc.GetByID(u.ID); err != nil {
		t.Fatalf("row must survive a refused delete: %v", err)
	}
}
