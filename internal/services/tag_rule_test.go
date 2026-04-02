package services

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mustCreateRule creates a tag rule and fails the test on error.
func mustCreateRule(t *testing.T, svc *TagRuleService, name, matchField, matchOp, matchValue, applyKey, applyValue string, priority int, createdBy int64) *TagRule {
	t.Helper()
	r, err := svc.Create(name, "", matchField, matchOp, matchValue, applyKey, applyValue, priority, createdBy)
	if err != nil {
		t.Fatalf("mustCreateRule(%s): %v", name, err)
	}
	return r
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func TestTagRuleCreate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "trcreate@example.com", "TR", "Create")
	svc := NewTagRuleService(db)

	r, err := svc.Create("Image tagger", "Tags all images", "content_type", "regex", "^image/", "type", "image", 10, user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if r.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if r.Name != "Image tagger" {
		t.Errorf("expected name 'Image tagger', got %q", r.Name)
	}
	if r.Description != "Tags all images" {
		t.Errorf("expected description 'Tags all images', got %q", r.Description)
	}
	if r.MatchField != "content_type" {
		t.Errorf("expected match_field 'content_type', got %q", r.MatchField)
	}
	if r.MatchOp != "regex" {
		t.Errorf("expected match_op 'regex', got %q", r.MatchOp)
	}
	if r.MatchValue != "^image/" {
		t.Errorf("expected match_value '^image/', got %q", r.MatchValue)
	}
	if r.ApplyKey != "type" {
		t.Errorf("expected apply_key 'type', got %q", r.ApplyKey)
	}
	if r.ApplyValue != "image" {
		t.Errorf("expected apply_value 'image', got %q", r.ApplyValue)
	}
	if r.Priority != 10 {
		t.Errorf("expected priority 10, got %d", r.Priority)
	}
	if !r.IsEnabled {
		t.Error("expected is_enabled=true by default")
	}
	if r.CreatedBy != user.ID {
		t.Errorf("expected created_by=%d, got %d", user.ID, r.CreatedBy)
	}
	if r.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestTagRuleGetByID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "trget@example.com", "TR", "Get")
	svc := NewTagRuleService(db)

	created := mustCreateRule(t, svc, "get-test", "filename", "contains", ".pdf", "format", "pdf", 5, user.ID)

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "get-test" {
		t.Errorf("expected name 'get-test', got %q", got.Name)
	}
	if got.ApplyKey != "format" {
		t.Errorf("expected apply_key 'format', got %q", got.ApplyKey)
	}
}

func TestTagRuleGetByIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewTagRuleService(db)

	_, err := svc.GetByID(99999)
	if !errors.Is(err, ErrTagRuleNotFound) {
		t.Errorf("expected ErrTagRuleNotFound, got %v", err)
	}
}

func TestTagRuleListOrderedByPriority(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "trlist@example.com", "TR", "List")
	svc := NewTagRuleService(db)

	// Insert out of priority order.
	mustCreateRule(t, svc, "low-prio", "visibility", "equals", "public", "vis", "pub", 50, user.ID)
	mustCreateRule(t, svc, "high-prio", "filename", "equals", "readme.md", "doc", "readme", 1, user.ID)
	mustCreateRule(t, svc, "mid-prio", "content_type", "equals", "text/plain", "format", "text", 20, user.ID)

	rules, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	if rules[0].Name != "high-prio" {
		t.Errorf("expected first rule 'high-prio', got %q", rules[0].Name)
	}
	if rules[1].Name != "mid-prio" {
		t.Errorf("expected second rule 'mid-prio', got %q", rules[1].Name)
	}
	if rules[2].Name != "low-prio" {
		t.Errorf("expected third rule 'low-prio', got %q", rules[2].Name)
	}
}

func TestTagRuleUpdate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "trupdate@example.com", "TR", "Update")
	svc := NewTagRuleService(db)

	r := mustCreateRule(t, svc, "before-update", "filename", "equals", "old.txt", "tag", "old", 10, user.ID)

	updated, err := svc.Update(r.ID, "after-update", "updated description", "content_type", "contains", "json", "format", "json", 5, true)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "after-update" {
		t.Errorf("expected name 'after-update', got %q", updated.Name)
	}
	if updated.Description != "updated description" {
		t.Errorf("expected description 'updated description', got %q", updated.Description)
	}
	if updated.MatchField != "content_type" {
		t.Errorf("expected match_field 'content_type', got %q", updated.MatchField)
	}
	if updated.MatchOp != "contains" {
		t.Errorf("expected match_op 'contains', got %q", updated.MatchOp)
	}
	if updated.MatchValue != "json" {
		t.Errorf("expected match_value 'json', got %q", updated.MatchValue)
	}
	if updated.ApplyKey != "format" {
		t.Errorf("expected apply_key 'format', got %q", updated.ApplyKey)
	}
	if updated.ApplyValue != "json" {
		t.Errorf("expected apply_value 'json', got %q", updated.ApplyValue)
	}
	if updated.Priority != 5 {
		t.Errorf("expected priority 5, got %d", updated.Priority)
	}
}

func TestTagRuleUpdateNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewTagRuleService(db)

	_, err := svc.Update(99999, "x", "", "filename", "equals", "x", "key", "val", 1, true)
	if !errors.Is(err, ErrTagRuleNotFound) {
		t.Errorf("expected ErrTagRuleNotFound, got %v", err)
	}
}

func TestTagRuleDelete(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "trdelete@example.com", "TR", "Delete")
	svc := NewTagRuleService(db)

	r := mustCreateRule(t, svc, "to-delete", "visibility", "equals", "public", "vis", "pub", 1, user.ID)

	if err := svc.Delete(r.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := svc.GetByID(r.ID)
	if !errors.Is(err, ErrTagRuleNotFound) {
		t.Errorf("expected ErrTagRuleNotFound after delete, got %v", err)
	}
}

func TestTagRuleDeleteNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewTagRuleService(db)

	err := svc.Delete(99999)
	if !errors.Is(err, ErrTagRuleNotFound) {
		t.Errorf("expected ErrTagRuleNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// EvaluateRules
// ---------------------------------------------------------------------------

func TestEvaluateRulesContentTypeEquals(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-ct-eq@example.com", "Eval", "CTEq")
	svc := NewTagRuleService(db)

	mustCreateRule(t, svc, "pdf-rule", "content_type", "equals", "application/pdf", "format", "pdf", 1, user.ID)

	tags, err := svc.EvaluateRules("report.pdf", "application/pdf", 1024, "private")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if tags["format"] != "pdf" {
		t.Errorf("expected format=pdf, got %q", tags["format"])
	}

	// Non-matching content type.
	tags2, err := svc.EvaluateRules("photo.jpg", "image/jpeg", 2048, "private")
	if err != nil {
		t.Fatalf("EvaluateRules non-match: %v", err)
	}
	if len(tags2) != 0 {
		t.Errorf("expected 0 tags for non-match, got %d", len(tags2))
	}
}

func TestEvaluateRulesContentTypeRegex(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-ct-re@example.com", "Eval", "CTRe")
	svc := NewTagRuleService(db)

	mustCreateRule(t, svc, "image-regex", "content_type", "regex", "^image/", "type", "image", 1, user.ID)

	for _, ct := range []string{"image/png", "image/jpeg", "image/gif"} {
		tags, err := svc.EvaluateRules("file.bin", ct, 100, "private")
		if err != nil {
			t.Fatalf("EvaluateRules(%s): %v", ct, err)
		}
		if tags["type"] != "image" {
			t.Errorf("expected type=image for %s, got %q", ct, tags["type"])
		}
	}

	// Non-matching: text/plain
	tags, _ := svc.EvaluateRules("file.txt", "text/plain", 100, "private")
	if _, ok := tags["type"]; ok {
		t.Error("expected no 'type' tag for text/plain")
	}
}

func TestEvaluateRulesFilenameRegex(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-fn-re@example.com", "Eval", "FNRe")
	svc := NewTagRuleService(db)

	mustCreateRule(t, svc, "invoice-regex", "filename", "regex", `(?i)^invoice`, "doc_type", "invoice", 1, user.ID)

	tags, err := svc.EvaluateRules("Invoice_2024.pdf", "application/pdf", 5000, "private")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if tags["doc_type"] != "invoice" {
		t.Errorf("expected doc_type=invoice, got %q", tags["doc_type"])
	}

	// Lower-case should also match due to (?i).
	tags2, _ := svc.EvaluateRules("invoice_march.xlsx", "application/vnd.ms-excel", 3000, "private")
	if tags2["doc_type"] != "invoice" {
		t.Errorf("expected doc_type=invoice for lowercase, got %q", tags2["doc_type"])
	}

	// Non-matching filename.
	tags3, _ := svc.EvaluateRules("receipt_2024.pdf", "application/pdf", 5000, "private")
	if _, ok := tags3["doc_type"]; ok {
		t.Error("expected no doc_type tag for non-matching filename")
	}
}

func TestEvaluateRulesFilenameContains(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-fn-ct@example.com", "Eval", "FNCt")
	svc := NewTagRuleService(db)

	mustCreateRule(t, svc, "backup-contains", "filename", "contains", "backup", "category", "backup", 1, user.ID)

	tags, err := svc.EvaluateRules("db_backup_2024.sql", "application/sql", 50000, "private")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if tags["category"] != "backup" {
		t.Errorf("expected category=backup, got %q", tags["category"])
	}

	// No match.
	tags2, _ := svc.EvaluateRules("production_dump.sql", "application/sql", 50000, "private")
	if _, ok := tags2["category"]; ok {
		t.Error("expected no category tag for non-matching filename")
	}
}

func TestEvaluateRulesFileSizeGtLt(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-sz@example.com", "Eval", "Size")
	svc := NewTagRuleService(db)

	// Rule: file_size > 1MB => large
	mustCreateRule(t, svc, "large-file", "file_size", "gt", "1048576", "size_class", "large", 1, user.ID)
	// Rule: file_size < 1024 => tiny
	mustCreateRule(t, svc, "tiny-file", "file_size", "lt", "1024", "size_class", "tiny", 2, user.ID)

	// 2 MB file should be "large".
	tags, err := svc.EvaluateRules("big.bin", "application/octet-stream", 2097152, "private")
	if err != nil {
		t.Fatalf("EvaluateRules gt: %v", err)
	}
	if tags["size_class"] != "large" {
		t.Errorf("expected size_class=large, got %q", tags["size_class"])
	}

	// 500 byte file should be "tiny".
	tags2, err := svc.EvaluateRules("small.txt", "text/plain", 500, "private")
	if err != nil {
		t.Fatalf("EvaluateRules lt: %v", err)
	}
	if tags2["size_class"] != "tiny" {
		t.Errorf("expected size_class=tiny, got %q", tags2["size_class"])
	}

	// 50 KB file matches neither.
	tags3, _ := svc.EvaluateRules("medium.bin", "application/octet-stream", 51200, "private")
	if _, ok := tags3["size_class"]; ok {
		t.Error("expected no size_class tag for medium file")
	}
}

func TestEvaluateRulesVisibilityEquals(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-vis@example.com", "Eval", "Vis")
	svc := NewTagRuleService(db)

	mustCreateRule(t, svc, "public-tag", "visibility", "equals", "public", "access", "open", 1, user.ID)

	tags, err := svc.EvaluateRules("readme.md", "text/markdown", 200, "public")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if tags["access"] != "open" {
		t.Errorf("expected access=open, got %q", tags["access"])
	}

	// Private should not match.
	tags2, _ := svc.EvaluateRules("secret.md", "text/markdown", 200, "private")
	if _, ok := tags2["access"]; ok {
		t.Error("expected no access tag for private visibility")
	}
}

func TestEvaluateRulesPriorityOrdering(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-prio@example.com", "Eval", "Prio")
	svc := NewTagRuleService(db)

	// Both rules match image/png and set "type", but priority 1 should win.
	mustCreateRule(t, svc, "specific", "content_type", "equals", "image/png", "type", "png", 1, user.ID)
	mustCreateRule(t, svc, "generic", "content_type", "regex", "^image/", "type", "image", 10, user.ID)

	tags, err := svc.EvaluateRules("photo.png", "image/png", 1000, "private")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if tags["type"] != "png" {
		t.Errorf("expected type=png (higher priority), got %q", tags["type"])
	}
}

func TestEvaluateRulesInvalidRegexDoesNotCrash(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-badre@example.com", "Eval", "BadRe")
	svc := NewTagRuleService(db)

	// Create a rule with a syntactically invalid regex. We need to bypass
	// validation, but since Create calls validateRule which only checks
	// field/op/key (not the regex syntax), the invalid pattern is accepted.
	mustCreateRule(t, svc, "bad-regex", "content_type", "regex", "[invalid(", "broken", "yes", 1, user.ID)

	// Should not panic; returns empty result for that rule.
	tags, err := svc.EvaluateRules("file.txt", "text/plain", 100, "private")
	if err != nil {
		t.Fatalf("EvaluateRules with bad regex: %v", err)
	}
	if _, ok := tags["broken"]; ok {
		t.Error("expected no 'broken' tag from invalid regex rule")
	}
}

func TestEvaluateRulesMultipleRulesMultipleTags(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-multi@example.com", "Eval", "Multi")
	svc := NewTagRuleService(db)

	// Three rules producing three different tag keys.
	mustCreateRule(t, svc, "img-type", "content_type", "regex", "^image/", "type", "image", 1, user.ID)
	mustCreateRule(t, svc, "large-file", "file_size", "gt", "1000000", "size_class", "large", 2, user.ID)
	mustCreateRule(t, svc, "public-access", "visibility", "equals", "public", "access", "open", 3, user.ID)

	// An upload matching all three.
	tags, err := svc.EvaluateRules("photo.jpg", "image/jpeg", 5000000, "public")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags["type"] != "image" {
		t.Errorf("expected type=image, got %q", tags["type"])
	}
	if tags["size_class"] != "large" {
		t.Errorf("expected size_class=large, got %q", tags["size_class"])
	}
	if tags["access"] != "open" {
		t.Errorf("expected access=open, got %q", tags["access"])
	}
}

func TestEvaluateRulesDisabledRulesSkipped(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "eval-disabled@example.com", "Eval", "Disabled")
	svc := NewTagRuleService(db)

	r := mustCreateRule(t, svc, "will-disable", "content_type", "equals", "text/plain", "format", "text", 1, user.ID)

	// Disable the rule.
	_, err := svc.Update(r.ID, r.Name, r.Description, r.MatchField, r.MatchOp, r.MatchValue, r.ApplyKey, r.ApplyValue, r.Priority, false)
	if err != nil {
		t.Fatalf("Update (disable): %v", err)
	}

	tags, err := svc.EvaluateRules("notes.txt", "text/plain", 100, "private")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if _, ok := tags["format"]; ok {
		t.Error("expected disabled rule to be skipped")
	}
}

func TestEvaluateRulesNoRulesReturnsEmpty(t *testing.T) {
	db := setupTestDB(t)
	svc := NewTagRuleService(db)

	tags, err := svc.EvaluateRules("anything.bin", "application/octet-stream", 1024, "private")
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags with no rules, got %d", len(tags))
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestValidateRuleInvalidMatchField(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "val-field@example.com", "Val", "Field")
	svc := NewTagRuleService(db)

	_, err := svc.Create("bad-field", "", "nonexistent_field", "equals", "x", "key", "val", 1, user.ID)
	if !errors.Is(err, ErrInvalidMatchField) {
		t.Errorf("expected ErrInvalidMatchField, got %v", err)
	}
}

func TestValidateRuleInvalidMatchOp(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "val-op@example.com", "Val", "Op")
	svc := NewTagRuleService(db)

	cases := []struct {
		name  string
		field string
		op    string
	}{
		{"regex for file_size", "file_size", "regex"},
		{"contains for file_size", "file_size", "contains"},
		{"equals for file_size", "file_size", "equals"},
		{"gt for content_type", "content_type", "gt"},
		{"lt for filename", "filename", "lt"},
		{"regex for visibility", "visibility", "regex"},
		{"contains for visibility", "visibility", "contains"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create("bad-op", "", tc.field, tc.op, "x", "key", "val", 1, user.ID)
			if !errors.Is(err, ErrInvalidMatchOp) {
				t.Errorf("expected ErrInvalidMatchOp for %s/%s, got %v", tc.field, tc.op, err)
			}
		})
	}
}

func TestValidateRuleInvalidApplyKey(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "val-key@example.com", "Val", "Key")
	svc := NewTagRuleService(db)

	badKeys := []string{
		"",            // empty
		"-starts",     // starts with hyphen
		".starts",     // starts with dot
		"_starts",     // starts with underscore
		"has spaces",  // spaces
		"has!bang",    // special char
		"a" + string(make([]byte, 63)), // 64 chars (1 start + 63 more = too long for {0,62})
	}

	for _, key := range badKeys {
		t.Run(key, func(t *testing.T) {
			_, err := svc.Create("bad-key", "", "filename", "equals", "x", key, "val", 1, user.ID)
			if !errors.Is(err, ErrInvalidApplyKey) {
				t.Errorf("expected ErrInvalidApplyKey for key %q, got %v", key, err)
			}
		})
	}
}

func TestValidateRuleValidApplyKeys(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "val-goodkey@example.com", "Val", "GoodKey")
	svc := NewTagRuleService(db)

	goodKeys := []string{
		"a",
		"env",
		"my.tag",
		"my-tag",
		"my_tag",
		"A1",
		"tag123.sub-key_v2",
	}

	for _, key := range goodKeys {
		t.Run(key, func(t *testing.T) {
			_, err := svc.Create("good-key-"+key, "", "filename", "equals", "x", key, "val", 1, user.ID)
			if err != nil {
				t.Errorf("expected valid apply_key %q to succeed, got %v", key, err)
			}
		})
	}
}

func TestValidateRuleOnUpdate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "val-update@example.com", "Val", "Update")
	svc := NewTagRuleService(db)

	r := mustCreateRule(t, svc, "valid-rule", "filename", "equals", "x", "key", "val", 1, user.ID)

	// Update with invalid field.
	_, err := svc.Update(r.ID, "updated", "", "bad_field", "equals", "x", "key", "val", 1, true)
	if !errors.Is(err, ErrInvalidMatchField) {
		t.Errorf("expected ErrInvalidMatchField on Update, got %v", err)
	}

	// Update with invalid op.
	_, err = svc.Update(r.ID, "updated", "", "file_size", "regex", "x", "key", "val", 1, true)
	if !errors.Is(err, ErrInvalidMatchOp) {
		t.Errorf("expected ErrInvalidMatchOp on Update, got %v", err)
	}

	// Update with invalid key.
	_, err = svc.Update(r.ID, "updated", "", "filename", "equals", "x", "", "val", 1, true)
	if !errors.Is(err, ErrInvalidApplyKey) {
		t.Errorf("expected ErrInvalidApplyKey on Update, got %v", err)
	}
}
