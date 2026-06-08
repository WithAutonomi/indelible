package services

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestBackupRoundTrip exports a catalog from one instance and imports it into a
// fresh one, asserting that the retrieval-critical fields (DataMap, address,
// visibility, owner, tags, collection membership) survive — the disaster-recovery
// guarantee. It also covers owner-email fallback and idempotent re-import.
func TestBackupRoundTrip(t *testing.T) {
	// ── Source instance ──
	src := setupTestDB(t)
	srcUsers := NewUserService(src)
	srcUploads := NewUploadService(src)
	srcTags := NewTagService(src)
	srcColls := NewCollectionService(src)
	srcBackup := NewBackupService(src)

	alice := createTestUser(t, srcUsers, "alice@test.local", "Alice", "A")
	bob := createTestUser(t, srcUsers, "bob@test.local", "Bob", "B")

	// A completed PRIVATE upload owned by alice — data_map is the retrieval secret.
	priv, err := srcUploads.Create(alice.ID, nil, "p.bin", "private.bin", 123, "application/octet-stream", "private", "/tmp/p", nil)
	if err != nil {
		t.Fatalf("create private upload: %v", err)
	}
	if err := srcUploads.MarkCompleted(priv.ID, "deadbeefdatamap", "0.01"); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	if err := srcTags.SetTags(priv.ID, map[string][]string{"project": {"apollo"}, "year": {"2026"}}); err != nil {
		t.Fatalf("set tags: %v", err)
	}
	coll, err := srcColls.Create("Reports", "", nil, alice.ID)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if err := srcColls.AddFile(coll.ID, priv.ID); err != nil {
		t.Fatalf("add file to collection: %v", err)
	}

	// A completed PUBLIC upload owned by bob — only the network address matters.
	pub, err := srcUploads.Create(bob.ID, nil, "q.bin", "public.bin", 456, "text/plain", "public", "/tmp/q", nil)
	if err != nil {
		t.Fatalf("create public upload: %v", err)
	}
	if err := srcUploads.MarkCompletedPublic(pub.ID, "cafef00daddress", "0.02"); err != nil {
		t.Fatalf("mark completed public: %v", err)
	}

	// ── Export ──
	var buf bytes.Buffer
	n, err := srcBackup.ExportUploads(&buf)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if n != 2 {
		t.Fatalf("exported %d uploads, want 2", n)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 { // header + 2 uploads
		t.Fatalf("export has %d lines, want 3 (header + 2)", len(lines))
	}
	var hdr ExportHeader
	if err := json.Unmarshal([]byte(lines[0]), &hdr); err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if hdr.Kind != uploadsExportKind || hdr.Schema != uploadsExportSchema || hdr.Count != 2 {
		t.Fatalf("header = %+v, want kind=%s schema=%d count=2", hdr, uploadsExportKind, uploadsExportSchema)
	}

	// ── Target instance (fresh DB) ──
	dst := setupTestDB(t)
	dstUsers := NewUserService(dst)
	dstUploads := NewUploadService(dst)
	dstTags := NewTagService(dst)
	dstColls := NewCollectionService(dst)
	dstBackup := NewBackupService(dst)

	admin := createTestUser(t, dstUsers, "admin@target.local", "Admin", "T")
	// alice exists on target (same email → matched); bob does NOT (→ fallback to admin).
	dstAlice := createTestUser(t, dstUsers, "alice@test.local", "Alice", "A")

	res, err := dstBackup.ImportUploads(strings.NewReader(buf.String()), admin.ID)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 2 || res.Skipped != 0 || res.OwnerFallback != 1 {
		t.Fatalf("import result = %+v, want imported=2 skipped=0 owner_fallback=1", res)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected import errors: %v", res.Errors)
	}

	// Private upload: DataMap + owner (matched by email) + status preserved.
	gotPriv, err := dstUploads.GetByUUID(priv.UUID)
	if err != nil {
		t.Fatalf("get imported private: %v", err)
	}
	if gotPriv.DataMap.String != "deadbeefdatamap" {
		t.Errorf("private DataMap = %q, want deadbeefdatamap", gotPriv.DataMap.String)
	}
	if gotPriv.UserID != dstAlice.ID {
		t.Errorf("private owner = %d, want alice %d (matched by email)", gotPriv.UserID, dstAlice.ID)
	}
	if gotPriv.Visibility != "private" || gotPriv.Status != "completed" {
		t.Errorf("private upload = visibility %q status %q, want private/completed", gotPriv.Visibility, gotPriv.Status)
	}

	tags, err := dstTags.GetTags(gotPriv.ID)
	if err != nil {
		t.Fatalf("get imported tags: %v", err)
	}
	if len(tags["project"]) != 1 || tags["project"][0] != "apollo" {
		t.Errorf("imported tags = %v, want project=[apollo]", tags)
	}

	collIDs, err := dstColls.CollectionIDsForUpload(gotPriv.ID)
	if err != nil {
		t.Fatalf("collection membership: %v", err)
	}
	if len(collIDs) != 1 {
		t.Errorf("imported private upload is in %d collections, want 1 (Reports)", len(collIDs))
	}

	// Public upload: address preserved, owner falls back to importer (bob absent).
	gotPub, err := dstUploads.GetByUUID(pub.UUID)
	if err != nil {
		t.Fatalf("get imported public: %v", err)
	}
	if gotPub.DatamapAddress.String != "cafef00daddress" {
		t.Errorf("public address = %q, want cafef00daddress", gotPub.DatamapAddress.String)
	}
	if gotPub.UserID != admin.ID {
		t.Errorf("public owner = %d, want importer %d (fallback)", gotPub.UserID, admin.ID)
	}

	// Re-import is idempotent: existing uuids are skipped, nothing re-created.
	res2, err := dstBackup.ImportUploads(strings.NewReader(buf.String()), admin.ID)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res2.Imported != 0 || res2.Skipped != 2 {
		t.Fatalf("re-import result = %+v, want imported=0 skipped=2", res2)
	}
}

// TestImportRejectsNonExport ensures a body without a valid header is rejected
// rather than silently importing garbage.
func TestImportRejectsNonExport(t *testing.T) {
	db := setupTestDB(t)
	bk := NewBackupService(db)

	_, err := bk.ImportUploads(strings.NewReader(`{"some":"json"}`+"\n"), 1)
	if err == nil {
		t.Fatal("expected import to reject a body with no valid header")
	}
}
