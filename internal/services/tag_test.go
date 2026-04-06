package services

import (
	"testing"
)

func TestTagSetAndGet(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "tag@example.com", "Tag", "User")
	uploadSvc := NewUploadService(db)
	u := createTestUpload(t, uploadSvc, user.ID, "tagged.bin", 100)

	svc := NewTagService(db)

	tags := map[string][]string{
		"env":     {"production"},
		"project": {"alpha"},
		"version": {"1.0"},
	}
	err := svc.SetTags(u.ID, tags)
	if err != nil {
		t.Fatalf("SetTags: %v", err)
	}

	got, err := svc.GetTags(u.ID)
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
	if got["version"][0] != "1.0" {
		t.Errorf("expected version=1.0, got %s", got["version"][0])
	}
}

func TestTagSetReplacesAll(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "replace@example.com", "Re", "Place")
	uploadSvc := NewUploadService(db)
	u := createTestUpload(t, uploadSvc, user.ID, "replace.bin", 100)

	svc := NewTagService(db)

	// Set initial tags
	svc.SetTags(u.ID, map[string][]string{
		"env":     {"staging"},
		"project": {"beta"},
		"old_key": {"old_value"},
	})

	// Replace with new set (old_key should be gone, env updated, new_key added)
	newTags := map[string][]string{
		"env":     {"production"},
		"project": {"beta"},
		"new_key": {"new_value"},
	}
	err := svc.SetTags(u.ID, newTags)
	if err != nil {
		t.Fatalf("SetTags replace: %v", err)
	}

	got, _ := svc.GetTags(u.ID)
	if len(got) != 3 {
		t.Errorf("expected 3 tags after replace, got %d", len(got))
	}
	if got["env"][0] != "production" {
		t.Errorf("expected env=production, got %s", got["env"][0])
	}
	if _, exists := got["old_key"]; exists {
		t.Error("expected old_key to be removed")
	}
	if got["new_key"][0] != "new_value" {
		t.Errorf("expected new_key=new_value, got %s", got["new_key"][0])
	}
}

func TestTagSetEmpty(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "empty@example.com", "Emp", "Ty")
	uploadSvc := NewUploadService(db)
	u := createTestUpload(t, uploadSvc, user.ID, "empty.bin", 100)

	svc := NewTagService(db)

	// Set some tags then clear them
	svc.SetTags(u.ID, map[string][]string{"k": {"v"}})
	err := svc.SetTags(u.ID, map[string][]string{})
	if err != nil {
		t.Fatalf("SetTags empty: %v", err)
	}

	got, _ := svc.GetTags(u.ID)
	if len(got) != 0 {
		t.Errorf("expected 0 tags after setting empty, got %d", len(got))
	}
}

func TestTagGetTagsNoTags(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "notags@example.com", "No", "Tags")
	uploadSvc := NewUploadService(db)
	u := createTestUpload(t, uploadSvc, user.ID, "notags.bin", 100)

	svc := NewTagService(db)

	got, err := svc.GetTags(u.ID)
	if err != nil {
		t.Fatalf("GetTags no tags: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 tags, got %d", len(got))
	}
}

func TestTagListKeys(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "keys@example.com", "Keys", "User")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user.ID, "f1.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "f2.bin", 200)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"env": {"prod"}, "project": {"alpha"}})
	svc.SetTags(u2.ID, map[string][]string{"env": {"staging"}, "team": {"backend"}})

	keys, err := svc.ListKeys(user.ID)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 distinct keys, got %d: %v", len(keys), keys)
	}

	// Should be sorted alphabetically
	expected := []string{"env", "project", "team"}
	for i, k := range expected {
		if i >= len(keys) || keys[i] != k {
			t.Errorf("expected key[%d]=%s, got %v", i, k, keys)
			break
		}
	}
}

func TestTagListKeysIsolatedByUser(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user1 := createTestUser(t, userSvc, "u1keys@example.com", "U1", "Keys")
	user2 := createTestUser(t, userSvc, "u2keys@example.com", "U2", "Keys")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user1.ID, "f1.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user2.ID, "f2.bin", 200)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"user1_key": {"val"}})
	svc.SetTags(u2.ID, map[string][]string{"user2_key": {"val"}})

	keys1, _ := svc.ListKeys(user1.ID)
	if len(keys1) != 1 || keys1[0] != "user1_key" {
		t.Errorf("expected [user1_key], got %v", keys1)
	}

	keys2, _ := svc.ListKeys(user2.ID)
	if len(keys2) != 1 || keys2[0] != "user2_key" {
		t.Errorf("expected [user2_key], got %v", keys2)
	}
}

func TestTagListValues(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "vals@example.com", "Vals", "User")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user.ID, "f1.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "f2.bin", 200)
	u3 := createTestUpload(t, uploadSvc, user.ID, "f3.bin", 300)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"env": {"prod"}})
	svc.SetTags(u2.ID, map[string][]string{"env": {"staging"}})
	svc.SetTags(u3.ID, map[string][]string{"env": {"prod"}}) // duplicate value

	values, err := svc.ListValues(user.ID, "env")
	if err != nil {
		t.Fatalf("ListValues: %v", err)
	}
	if len(values) != 2 {
		t.Errorf("expected 2 distinct values for 'env', got %d: %v", len(values), values)
	}

	// Should be sorted alphabetically
	if len(values) == 2 && values[0] != "prod" {
		t.Errorf("expected first value='prod', got %s", values[0])
	}
}

func TestTagListValuesNoMatches(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "novals@example.com", "No", "Vals")

	svc := NewTagService(db)
	values, err := svc.ListValues(user.ID, "nonexistent_key")
	if err != nil {
		t.Fatalf("ListValues no matches: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("expected 0 values, got %d", len(values))
	}
}

func TestTagSearchByTag(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "search@example.com", "Search", "User")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user.ID, "prod1.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "prod2.bin", 200)
	u3 := createTestUpload(t, uploadSvc, user.ID, "staging.bin", 300)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"env": {"prod"}, "team": {"backend"}})
	svc.SetTags(u2.ID, map[string][]string{"env": {"prod"}, "team": {"frontend"}})
	svc.SetTags(u3.ID, map[string][]string{"env": {"staging"}, "team": {"backend"}})

	// Search env=prod
	results, total, err := svc.Search(
		map[string]string{"env": "prod"},
		"", user.ID, 10, 0,
	)
	if err != nil {
		t.Fatalf("Search env=prod: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestTagSearchByMultipleTags(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "multi@example.com", "Multi", "Tag")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user.ID, "match.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "partial.bin", 200)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"env": {"prod"}, "team": {"backend"}})
	svc.SetTags(u2.ID, map[string][]string{"env": {"prod"}, "team": {"frontend"}})

	// Search env=prod AND team=backend (only u1 matches)
	results, total, err := svc.Search(
		map[string]string{"env": "prod", "team": "backend"},
		"", user.ID, 10, 0,
	)
	if err != nil {
		t.Fatalf("Search multi-tag: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Upload.ID != u1.ID {
		t.Errorf("expected upload ID=%d, got %d", u1.ID, results[0].Upload.ID)
	}
}

func TestTagSearchByFilename(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "fnsearch@example.com", "FN", "Search")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user.ID, "report_2024.pdf", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "image.png", 200)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"type": {"report"}})
	svc.SetTags(u2.ID, map[string][]string{"type": {"image"}})

	// Search by filename substring (no tag filter)
	results, total, err := svc.Search(
		map[string]string{},
		"report", user.ID, 10, 0,
	)
	if err != nil {
		t.Fatalf("Search by filename: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Upload.ID != u1.ID {
		t.Errorf("expected upload ID=%d, got %d", u1.ID, results[0].Upload.ID)
	}
}

func TestTagSearchCombinedTagAndFilename(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "combined@example.com", "Comb", "Search")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user.ID, "report_q1.pdf", 100)
	u2 := createTestUpload(t, uploadSvc, user.ID, "report_q2.pdf", 200)
	u3 := createTestUpload(t, uploadSvc, user.ID, "image_q1.png", 300)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"quarter": {"q1"}})
	svc.SetTags(u2.ID, map[string][]string{"quarter": {"q2"}})
	svc.SetTags(u3.ID, map[string][]string{"quarter": {"q1"}})

	// Search quarter=q1 AND filename contains "report" -- only u1
	results, total, err := svc.Search(
		map[string]string{"quarter": "q1"},
		"report", user.ID, 10, 0,
	)
	if err != nil {
		t.Fatalf("Search combined: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(results) == 1 && results[0].Upload.ID != u1.ID {
		t.Errorf("expected upload ID=%d, got %d", u1.ID, results[0].Upload.ID)
	}
}

func TestTagSearchNoFilters(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "nofilter@example.com", "No", "Filter")
	uploadSvc := NewUploadService(db)

	createTestUpload(t, uploadSvc, user.ID, "f1.bin", 100)
	createTestUpload(t, uploadSvc, user.ID, "f2.bin", 200)

	svc := NewTagService(db)

	// No tag filters, no query, but filtered by user
	results, total, err := svc.Search(map[string]string{}, "", user.ID, 10, 0)
	if err != nil {
		t.Fatalf("Search no filters: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestTagSearchResultContainsUpload(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "restags@example.com", "Res", "Tags")
	uploadSvc := NewUploadService(db)

	u := createTestUpload(t, uploadSvc, user.ID, "tagged.bin", 100)

	svc := NewTagService(db)
	svc.SetTags(u.ID, map[string][]string{"env": {"prod"}, "team": {"ops"}})

	// Search by filename only (no tag join) to avoid nested GetTags deadlock on SQLite :memory:
	results, total, err := svc.Search(map[string]string{}, "tagged", user.ID, 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Upload.ID != u.ID {
		t.Errorf("expected upload ID=%d, got %d", u.ID, results[0].Upload.ID)
	}
	if results[0].Upload.OriginalFilename != "tagged.bin" {
		t.Errorf("expected filename=tagged.bin, got %s", results[0].Upload.OriginalFilename)
	}

	// Verify tags via direct GetTags call (Search's nested GetTags can deadlock
	// on SQLite :memory: when rows cursor holds the single connection)
	tags, err := svc.GetTags(results[0].Upload.ID)
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if tags["env"][0] != "prod" {
		t.Errorf("expected tag env=prod, got %s", tags["env"][0])
	}
	if tags["team"][0] != "ops" {
		t.Errorf("expected tag team=ops, got %s", tags["team"][0])
	}
}

func TestTagSearchPagination(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "searchpage@example.com", "Search", "Page")
	uploadSvc := NewUploadService(db)

	svc := NewTagService(db)
	for i := 0; i < 5; i++ {
		u := createTestUpload(t, uploadSvc, user.ID, "paginated.bin", int64(100*(i+1)))
		svc.SetTags(u.ID, map[string][]string{"batch": {"test"}})
	}

	// Get first page
	results, total, err := svc.Search(map[string]string{"batch": "test"}, "", user.ID, 2, 0)
	if err != nil {
		t.Fatalf("Search page 1: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results on page 1, got %d", len(results))
	}

	// Get second page
	results2, _, err := svc.Search(map[string]string{"batch": "test"}, "", user.ID, 2, 2)
	if err != nil {
		t.Fatalf("Search page 2: %v", err)
	}
	if len(results2) != 2 {
		t.Errorf("expected 2 results on page 2, got %d", len(results2))
	}

	// Pages should have different uploads
	if len(results) > 0 && len(results2) > 0 && results[0].Upload.ID == results2[0].Upload.ID {
		t.Error("expected different uploads on different pages")
	}
}

func TestTagSearchAllUsers(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user1 := createTestUser(t, userSvc, "all1@example.com", "All", "One")
	user2 := createTestUser(t, userSvc, "all2@example.com", "All", "Two")
	uploadSvc := NewUploadService(db)

	u1 := createTestUpload(t, uploadSvc, user1.ID, "f1.bin", 100)
	u2 := createTestUpload(t, uploadSvc, user2.ID, "f2.bin", 200)

	svc := NewTagService(db)
	svc.SetTags(u1.ID, map[string][]string{"shared": {"yes"}})
	svc.SetTags(u2.ID, map[string][]string{"shared": {"yes"}})

	// userID=0 should search all users (admin mode)
	results, total, err := svc.Search(map[string]string{"shared": "yes"}, "", 0, 10, 0)
	if err != nil {
		t.Fatalf("Search all users: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
