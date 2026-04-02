package services

import (
	"testing"
)

func TestCollectionTagsSetAndGet(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "colltag@example.com", "Coll", "Tag")

	collSvc := NewCollectionService(db)
	coll, err := collSvc.Create("Tagged Collection", "has tags", nil, user.ID)
	if err != nil {
		t.Fatalf("Create collection: %v", err)
	}

	svc := NewCollectionTagService(db)

	tags := map[string][]string{
		"env":     {"production"},
		"project": {"alpha"},
		"version": {"2.0"},
	}
	err = svc.SetTags(coll.ID, tags)
	if err != nil {
		t.Fatalf("SetTags: %v", err)
	}

	got, err := svc.GetTags(coll.ID)
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 tags, got %d", len(got))
	}
	if got["env"][0] != "production" {
		t.Errorf("expected env=production, got %s", got["env"][0])
	}
	if got["project"][0] != "alpha" {
		t.Errorf("expected project=alpha, got %s", got["project"][0])
	}
	if got["version"][0] != "2.0" {
		t.Errorf("expected version=2.0, got %s", got["version"][0])
	}
}

func TestCollectionTagsReplace(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "collreplace@example.com", "Coll", "Replace")

	collSvc := NewCollectionService(db)
	coll, err := collSvc.Create("Replace Tags", "", nil, user.ID)
	if err != nil {
		t.Fatalf("Create collection: %v", err)
	}

	svc := NewCollectionTagService(db)

	// Set initial tags
	err = svc.SetTags(coll.ID, map[string][]string{
		"env":     {"staging"},
		"project": {"beta"},
		"old_key": {"old_value"},
	})
	if err != nil {
		t.Fatalf("SetTags initial: %v", err)
	}

	// Replace with new set
	err = svc.SetTags(coll.ID, map[string][]string{
		"env":     {"production"},
		"project": {"beta"},
		"new_key": {"new_value"},
	})
	if err != nil {
		t.Fatalf("SetTags replace: %v", err)
	}

	got, err := svc.GetTags(coll.ID)
	if err != nil {
		t.Fatalf("GetTags after replace: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 tags after replace, got %d", len(got))
	}
	if got["env"][0] != "production" {
		t.Errorf("expected env=production, got %s", got["env"][0])
	}
	if got["project"][0] != "beta" {
		t.Errorf("expected project=beta, got %s", got["project"][0])
	}
	if _, exists := got["old_key"]; exists {
		t.Error("expected old_key to be removed after replace")
	}
	if got["new_key"][0] != "new_value" {
		t.Errorf("expected new_key=new_value, got %s", got["new_key"][0])
	}
}

func TestCollectionTagsInheritToFile(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "collinherit@example.com", "Coll", "Inherit")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, err := collSvc.Create("Inheritable", "", nil, user.ID)
	if err != nil {
		t.Fatalf("Create collection: %v", err)
	}
	upload := createTestUpload(t, uploadSvc, user.ID, "inherited.bin", 100)

	collTagSvc := NewCollectionTagService(db)
	fileTagSvc := NewTagService(db)

	// Set collection tags
	err = collTagSvc.SetTags(coll.ID, map[string][]string{
		"env":     {"production"},
		"project": {"alpha"},
	})
	if err != nil {
		t.Fatalf("SetTags on collection: %v", err)
	}

	// Inherit to file
	added, err := collTagSvc.InheritToFile(coll.ID, upload.ID)
	if err != nil {
		t.Fatalf("InheritToFile: %v", err)
	}
	if added != 2 {
		t.Errorf("expected 2 tags added, got %d", added)
	}

	// Verify file has the tags
	fileTags, err := fileTagSvc.GetTags(upload.ID)
	if err != nil {
		t.Fatalf("GetTags on file: %v", err)
	}
	if len(fileTags) != 2 {
		t.Errorf("expected 2 file tags, got %d", len(fileTags))
	}
	if fileTags["env"][0] != "production" {
		t.Errorf("expected file env=production, got %s", fileTags["env"][0])
	}
	if fileTags["project"][0] != "alpha" {
		t.Errorf("expected file project=alpha, got %s", fileTags["project"][0])
	}
}

func TestCollectionTagsInheritNoOverwrite(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "collnooverwrite@example.com", "Coll", "NoOver")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, err := collSvc.Create("NoOverwrite", "", nil, user.ID)
	if err != nil {
		t.Fatalf("Create collection: %v", err)
	}
	upload := createTestUpload(t, uploadSvc, user.ID, "existing_tags.bin", 200)

	collTagSvc := NewCollectionTagService(db)
	fileTagSvc := NewTagService(db)

	// Set a tag on the file first
	err = fileTagSvc.SetTags(upload.ID, map[string][]string{
		"env":    {"staging"},
		"custom": {"file_only"},
	})
	if err != nil {
		t.Fatalf("SetTags on file: %v", err)
	}

	// Set collection tags with a conflicting key (env) and a new key (project)
	err = collTagSvc.SetTags(coll.ID, map[string][]string{
		"env":     {"production"},
		"project": {"alpha"},
	})
	if err != nil {
		t.Fatalf("SetTags on collection: %v", err)
	}

	// Inherit to file -- should NOT overwrite env
	added, err := collTagSvc.InheritToFile(coll.ID, upload.ID)
	if err != nil {
		t.Fatalf("InheritToFile: %v", err)
	}
	if added != 1 {
		t.Errorf("expected 1 tag added (project only), got %d", added)
	}

	// Verify file tags: env should still be "staging" (not overwritten),
	// project should be "alpha" (inherited), custom should remain
	fileTags, err := fileTagSvc.GetTags(upload.ID)
	if err != nil {
		t.Fatalf("GetTags on file: %v", err)
	}
	if fileTags["env"][0] != "staging" {
		t.Errorf("expected file env=staging (not overwritten), got %s", fileTags["env"][0])
	}
	if fileTags["project"][0] != "alpha" {
		t.Errorf("expected file project=alpha (inherited), got %s", fileTags["project"][0])
	}
	if fileTags["custom"][0] != "file_only" {
		t.Errorf("expected file custom=file_only (preserved), got %s", fileTags["custom"][0])
	}
	if len(fileTags) != 3 {
		t.Errorf("expected 3 total file tags, got %d", len(fileTags))
	}
}
