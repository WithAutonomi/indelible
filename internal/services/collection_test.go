package services

import (
	"testing"
)

func TestCollectionCreate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "coll@example.com", "Coll", "User")

	svc := NewCollectionService(db)
	c, err := svc.Create("My Collection", "A test collection", nil, user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if c.Name != "My Collection" {
		t.Errorf("expected name='My Collection', got %s", c.Name)
	}
	if c.Description != "A test collection" {
		t.Errorf("expected description='A test collection', got %s", c.Description)
	}
	if c.ParentID.Valid {
		t.Error("expected parent_id to be NULL for top-level collection")
	}
	if c.CreatedBy != user.ID {
		t.Errorf("expected created_by=%d, got %d", user.ID, c.CreatedBy)
	}
	if c.FileCount != 0 {
		t.Errorf("expected file_count=0, got %d", c.FileCount)
	}
}

func TestCollectionCreateWithParent(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "parent@example.com", "Par", "Ent")

	svc := NewCollectionService(db)
	parent, err := svc.Create("Parent", "", nil, user.ID)
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	child, err := svc.Create("Child", "inside parent", &parent.ID, user.ID)
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}
	if !child.ParentID.Valid || child.ParentID.Int64 != parent.ID {
		t.Errorf("expected parent_id=%d, got %v", parent.ID, child.ParentID)
	}
}

func TestCollectionCreateWithNonexistentParent(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "noparent@example.com", "No", "Parent")

	svc := NewCollectionService(db)
	fakeParent := int64(99999)
	_, err := svc.Create("Orphan", "", &fakeParent, user.ID)
	if err != ErrCollectionNotFound {
		t.Errorf("expected ErrCollectionNotFound, got %v", err)
	}
}

func TestCollectionGetByID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "getc@example.com", "Get", "Coll")

	svc := NewCollectionService(db)
	created, _ := svc.Create("GetTest", "desc", nil, user.ID)

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "GetTest" {
		t.Errorf("expected name='GetTest', got %s", got.Name)
	}
}

func TestCollectionGetByIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewCollectionService(db)

	_, err := svc.GetByID(99999)
	if err != ErrCollectionNotFound {
		t.Errorf("expected ErrCollectionNotFound, got %v", err)
	}
}

func TestCollectionGetByIDWithFileCount(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "fcount@example.com", "FC", "User")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("WithFiles", "", nil, user.ID)
	u1 := createTestUpload(t, uploadSvc, user.ID, "f1.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "f2.bin", 200)
	collSvc.AddFile(coll.ID, u1.ID)
	collSvc.AddFile(coll.ID, u2.ID)

	got, err := collSvc.GetByID(coll.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.FileCount != 2 {
		t.Errorf("expected file_count=2, got %d", got.FileCount)
	}
}

func TestCollectionList(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "listc@example.com", "List", "Coll")
	otherUser := createTestUser(t, userSvc, "other@example.com", "Other", "User")

	svc := NewCollectionService(db)
	svc.Create("Alpha", "", nil, user.ID)
	svc.Create("Beta", "", nil, user.ID)
	svc.Create("OthersColl", "", nil, otherUser.ID)

	// List top-level for user (parentID=nil)
	colls, err := svc.List(user.ID, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(colls) != 2 {
		t.Errorf("expected 2 collections for user, got %d", len(colls))
	}

	// Verify alphabetical order
	if len(colls) == 2 && colls[0].Name != "Alpha" {
		t.Errorf("expected first collection='Alpha', got %s", colls[0].Name)
	}
}

func TestCollectionListByParent(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "listp@example.com", "List", "Parent")

	svc := NewCollectionService(db)
	parent, _ := svc.Create("ParentFolder", "", nil, user.ID)
	svc.Create("Child1", "", &parent.ID, user.ID)
	svc.Create("Child2", "", &parent.ID, user.ID)
	svc.Create("TopLevel", "", nil, user.ID)

	// List children of parent
	children, err := svc.List(user.ID, &parent.ID)
	if err != nil {
		t.Fatalf("List by parent: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}

	// Top-level should not include children
	topLevel, _ := svc.List(user.ID, nil)
	if len(topLevel) != 2 {
		t.Errorf("expected 2 top-level, got %d", len(topLevel))
	}
}

func TestCollectionUpdate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "updc@example.com", "Upd", "Coll")

	svc := NewCollectionService(db)
	c, _ := svc.Create("Original", "old desc", nil, user.ID)

	updated, err := svc.Update(c.ID, "Renamed", "new description")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("expected name='Renamed', got %s", updated.Name)
	}
	if updated.Description != "new description" {
		t.Errorf("expected description='new description', got %s", updated.Description)
	}
}

func TestCollectionDelete(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "delc@example.com", "Del", "Coll")

	svc := NewCollectionService(db)
	c, _ := svc.Create("ToDelete", "", nil, user.ID)

	err := svc.Delete(c.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.GetByID(c.ID)
	if err != ErrCollectionNotFound {
		t.Errorf("expected ErrCollectionNotFound after delete, got %v", err)
	}
}

func TestCollectionDeleteCascadesChildren(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cascade@example.com", "Cas", "Cade")

	svc := NewCollectionService(db)
	parent, _ := svc.Create("Parent", "", nil, user.ID)
	child, _ := svc.Create("Child", "", &parent.ID, user.ID)
	grandchild, _ := svc.Create("Grandchild", "", &child.ID, user.ID)

	// Delete parent should cascade
	err := svc.Delete(parent.ID)
	if err != nil {
		t.Fatalf("Delete parent: %v", err)
	}

	// All should be gone
	_, err = svc.GetByID(parent.ID)
	if err != ErrCollectionNotFound {
		t.Error("expected parent to be deleted")
	}
	_, err = svc.GetByID(child.ID)
	if err != ErrCollectionNotFound {
		t.Error("expected child to be deleted")
	}
	_, err = svc.GetByID(grandchild.ID)
	if err != ErrCollectionNotFound {
		t.Error("expected grandchild to be deleted")
	}
}

func TestCollectionDeleteCleansUpFileAssociations(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "delfiles@example.com", "Del", "Files")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("WithFiles", "", nil, user.ID)
	u := createTestUpload(t, uploadSvc, user.ID, "f.bin", 100)
	collSvc.AddFile(coll.ID, u.ID)

	err := collSvc.Delete(coll.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// File association should be gone
	ids, _ := collSvc.CollectionIDsForUpload(u.ID)
	if len(ids) != 0 {
		t.Error("expected collection_files to be cleaned up after collection delete")
	}

	// Upload itself should still exist
	_, err = uploadSvc.GetByID(u.ID)
	if err != nil {
		t.Error("expected upload to still exist after collection delete")
	}
}

func TestCollectionAddFile(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "addf@example.com", "Add", "File")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("Folder", "", nil, user.ID)
	u := createTestUpload(t, uploadSvc, user.ID, "doc.pdf", 500)

	err := collSvc.AddFile(coll.ID, u.ID)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}

	// Verify file count
	got, _ := collSvc.GetByID(coll.ID)
	if got.FileCount != 1 {
		t.Errorf("expected file_count=1, got %d", got.FileCount)
	}
}

func TestCollectionAddFileDuplicate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "dupf@example.com", "Dup", "File")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("Folder", "", nil, user.ID)
	u := createTestUpload(t, uploadSvc, user.ID, "doc.pdf", 500)

	collSvc.AddFile(coll.ID, u.ID)
	err := collSvc.AddFile(coll.ID, u.ID)
	if err != ErrFileAlreadyInCollection {
		t.Errorf("expected ErrFileAlreadyInCollection, got %v", err)
	}
}

func TestCollectionRemoveFile(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "rmf@example.com", "Rm", "File")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("Folder", "", nil, user.ID)
	u := createTestUpload(t, uploadSvc, user.ID, "doc.pdf", 500)
	collSvc.AddFile(coll.ID, u.ID)

	err := collSvc.RemoveFile(coll.ID, u.ID)
	if err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	got, _ := collSvc.GetByID(coll.ID)
	if got.FileCount != 0 {
		t.Errorf("expected file_count=0 after remove, got %d", got.FileCount)
	}
}

func TestCollectionRemoveFileNotInCollection(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "rmnotf@example.com", "RmNot", "File")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("Folder", "", nil, user.ID)
	u := createTestUpload(t, uploadSvc, user.ID, "doc.pdf", 500)

	err := collSvc.RemoveFile(coll.ID, u.ID)
	if err != ErrFileNotInCollection {
		t.Errorf("expected ErrFileNotInCollection, got %v", err)
	}
}

func TestCollectionListFiles(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "listf@example.com", "List", "Files")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("Folder", "", nil, user.ID)
	u1 := createTestUpload(t, uploadSvc, user.ID, "first.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "second.bin", 200)
	u3 := createTestUpload(t, uploadSvc, user.ID, "third.bin", 300)

	collSvc.AddFile(coll.ID, u1.ID)
	collSvc.AddFile(coll.ID, u2.ID)
	collSvc.AddFile(coll.ID, u3.ID)

	files, total, err := collSvc.ListFiles(coll.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestCollectionListFilesPaginated(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "pagef@example.com", "Page", "Files")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	coll, _ := collSvc.Create("Folder", "", nil, user.ID)
	for i := 0; i < 5; i++ {
		u := createTestUpload(t, uploadSvc, user.ID, "file"+string(rune('a'+i))+".bin", int64(100*(i+1)))
		collSvc.AddFile(coll.ID, u.ID)
	}

	files, total, err := collSvc.ListFiles(coll.ID, 2, 0)
	if err != nil {
		t.Fatalf("ListFiles paginated: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files on page, got %d", len(files))
	}
}

func TestCollectionIDsForUpload(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "ids@example.com", "IDs", "User")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	c1, _ := collSvc.Create("Folder1", "", nil, user.ID)
	c2, _ := collSvc.Create("Folder2", "", nil, user.ID)
	u := createTestUpload(t, uploadSvc, user.ID, "shared.bin", 100)

	collSvc.AddFile(c1.ID, u.ID)
	collSvc.AddFile(c2.ID, u.ID)

	ids, err := collSvc.CollectionIDsForUpload(u.ID)
	if err != nil {
		t.Fatalf("CollectionIDsForUpload: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 collection IDs, got %d", len(ids))
	}

	// Check both IDs are present
	found := map[int64]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found[c1.ID] || !found[c2.ID] {
		t.Errorf("expected collection IDs %d and %d, got %v", c1.ID, c2.ID, ids)
	}
}

func TestCollectionIDsForUploadEmpty(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "noids@example.com", "No", "IDs")

	collSvc := NewCollectionService(db)
	uploadSvc := NewUploadService(db)

	u := createTestUpload(t, uploadSvc, user.ID, "lonely.bin", 100)

	ids, err := collSvc.CollectionIDsForUpload(u.ID)
	if err != nil {
		t.Fatalf("CollectionIDsForUpload: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 collection IDs, got %d", len(ids))
	}
}

func TestCollectionNestedHierarchy(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "nested@example.com", "Nested", "User")

	svc := NewCollectionService(db)

	// Build: Root -> Mid -> Leaf
	root, err := svc.Create("Root", "", nil, user.ID)
	if err != nil {
		t.Fatalf("Create root: %v", err)
	}
	mid, err := svc.Create("Mid", "", &root.ID, user.ID)
	if err != nil {
		t.Fatalf("Create mid: %v", err)
	}
	leaf, err := svc.Create("Leaf", "", &mid.ID, user.ID)
	if err != nil {
		t.Fatalf("Create leaf: %v", err)
	}

	// Verify parent chain
	if !mid.ParentID.Valid || mid.ParentID.Int64 != root.ID {
		t.Errorf("mid parent should be root")
	}
	if !leaf.ParentID.Valid || leaf.ParentID.Int64 != mid.ID {
		t.Errorf("leaf parent should be mid")
	}

	// Top-level should only have Root
	topLevel, _ := svc.List(user.ID, nil)
	if len(topLevel) != 1 || topLevel[0].ID != root.ID {
		t.Error("expected only Root at top level")
	}

	// Mid's children should only have Leaf
	midChildren, _ := svc.List(user.ID, &mid.ID)
	if len(midChildren) != 1 || midChildren[0].ID != leaf.ID {
		t.Error("expected only Leaf under Mid")
	}
}

func TestCollectionNestedDeletePartial(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "partial@example.com", "Part", "Del")

	svc := NewCollectionService(db)
	root, _ := svc.Create("Root", "", nil, user.ID)
	mid, _ := svc.Create("Mid", "", &root.ID, user.ID)
	svc.Create("Leaf", "", &mid.ID, user.ID)

	// Delete mid should cascade to leaf but leave root
	err := svc.Delete(mid.ID)
	if err != nil {
		t.Fatalf("Delete mid: %v", err)
	}

	// Root should still exist
	_, err = svc.GetByID(root.ID)
	if err != nil {
		t.Error("expected root to still exist")
	}

	// Mid and Leaf should be gone
	_, err = svc.GetByID(mid.ID)
	if err != ErrCollectionNotFound {
		t.Error("expected mid to be deleted")
	}
}
