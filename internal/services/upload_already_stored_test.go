package services

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/downloadcache"
)

// V2-875: 'already_stored' (the V2-399 dedup status) was written by the
// worker but absent from the schema's status CHECK constraint — on any
// database built from the migrations, the transition itself failed. These
// tests run against the freshly-migrated schema in both CI dialects, so they
// are the direct regression: they fail on the pre-014 constraint.

func TestMarkAlreadyStoredPersists(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dedup@example.com", "D", "P")
	svc := NewUploadService(db)

	private := createTestUpload(t, svc, user.ID, "dedup-private.bin", 10)
	if err := svc.MarkAlreadyStored(private.ID, "dm-dedup-private", "0"); err != nil {
		t.Fatalf("MarkAlreadyStored violated the schema: %v", err)
	}
	got, err := svc.GetByID(private.ID)
	if err != nil || got.Status != "already_stored" {
		t.Fatalf("status = %q (err=%v), want already_stored", got.Status, err)
	}
	if key := cacheKeyOf(t, svc, private.ID); key != downloadcache.KeyForIdentifier("dm-dedup-private") {
		t.Fatalf("already_stored must stamp cache_key; got %q", key)
	}

	public := createTestUpload(t, svc, user.ID, "dedup-public.bin", 10)
	if err := svc.MarkAlreadyStoredPublic(public.ID, "addr-dedup-public", "0"); err != nil {
		t.Fatalf("MarkAlreadyStoredPublic violated the schema: %v", err)
	}
	if got, err := svc.GetByID(public.ID); err != nil || got.Status != "already_stored" {
		t.Fatalf("public status = %q (err=%v), want already_stored", got.Status, err)
	}
}

// The second half of V2-875: already_stored uploads were undeletable (the
// delete's status list omitted them), which since V2-824/873 also meant no
// erasure path for their DataMap row or cached bytes.
func TestDeleteAlreadyStoredUpload(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dedup-del@example.com", "D", "D")
	svc := NewUploadService(db)

	u := createTestUpload(t, svc, user.ID, "dedup-del.bin", 10)
	if err := svc.MarkAlreadyStored(u.ID, "dm-dedup-del", "0"); err != nil {
		t.Fatalf("MarkAlreadyStored: %v", err)
	}

	if err := svc.Delete(u.ID); err != nil {
		t.Fatalf("already_stored upload must be deletable: %v", err)
	}
	if _, err := svc.GetByID(u.ID); err != ErrUploadNotFound {
		t.Fatalf("row survived the delete: %v", err)
	}
	// The purge fan-out ran like any other delete.
	entries, err := svc.PurgeLogSince(0, 10)
	if err != nil || len(entries) != 1 || entries[0].CacheKey != downloadcache.KeyForIdentifier("dm-dedup-del") {
		t.Fatalf("purge log = %+v (err=%v), want the dedup key exactly once", entries, err)
	}
}

// Review follow-up: the public variant's delete → purge fan-out, keyed on the
// network-address derivation rather than the DataMap.
func TestDeleteAlreadyStoredPublicUpload(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dedup-del-pub@example.com", "D", "P")
	svc := NewUploadService(db)

	u := createTestUpload(t, svc, user.ID, "dedup-del-pub.bin", 10)
	if err := svc.MarkAlreadyStoredPublic(u.ID, "addr-dedup-del-pub", "0"); err != nil {
		t.Fatalf("MarkAlreadyStoredPublic: %v", err)
	}

	if err := svc.Delete(u.ID); err != nil {
		t.Fatalf("public already_stored upload must be deletable: %v", err)
	}
	if _, err := svc.GetByID(u.ID); err != ErrUploadNotFound {
		t.Fatalf("row survived the delete: %v", err)
	}
	entries, err := svc.PurgeLogSince(0, 10)
	if err != nil || len(entries) != 1 || entries[0].CacheKey != downloadcache.KeyForIdentifier("addr-dedup-del-pub") {
		t.Fatalf("purge log = %+v (err=%v), want the address-derived key exactly once", entries, err)
	}
}
