package services

import (
	"testing"
	"time"
)

func TestLogWriteAudit(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	uid := user.ID
	err := svc.WriteAudit("login", "info", &uid, "User logged in", "127.0.0.1", "TestAgent", "req-test-1")
	if err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	entries, total, err := svc.QueryAuditLogs("", "", nil, nil, nil, 50, 0)
	if err != nil {
		t.Fatalf("QueryAuditLogs: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].EventType != "login" {
		t.Errorf("EventType = %q", entries[0].EventType)
	}
	if entries[0].Severity != "info" {
		t.Errorf("Severity = %q", entries[0].Severity)
	}
}

func TestLogWriteAudit_NilUserID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	err := svc.WriteAudit("system_start", "info", nil, "System started", "", "", "")
	if err != nil {
		t.Fatalf("WriteAudit with nil userID: %v", err)
	}

	entries, _, _ := svc.QueryAuditLogs("system_start", "", nil, nil, nil, 50, 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].UserID.Valid {
		t.Error("user_id should be NULL")
	}
}

func TestLogWriteSystem(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	err := svc.WriteSystem("error", "uploader", "Upload failed", "file too large", "")
	if err != nil {
		t.Fatalf("WriteSystem: %v", err)
	}

	entries, total, err := svc.QuerySystemLogs("", "", nil, nil, 50, 0)
	if err != nil {
		t.Fatalf("QuerySystemLogs: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if entries[0].Level != "error" {
		t.Errorf("Level = %q", entries[0].Level)
	}
	if entries[0].Component != "uploader" {
		t.Errorf("Component = %q", entries[0].Component)
	}
	if !entries[0].Detail.Valid || entries[0].Detail.String != "file too large" {
		t.Errorf("Detail = %v", entries[0].Detail)
	}
}

func TestLogWriteSystem_EmptyDetail(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	svc.WriteSystem("info", "startup", "Service started", "", "")

	entries, _, _ := svc.QuerySystemLogs("", "", nil, nil, 50, 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Detail.Valid {
		t.Error("empty detail should be NULL")
	}
}

func TestLogQueryAuditLogs_FilterByEventType(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	svc.WriteAudit("login", "info", nil, "logged in", "", "", "")
	svc.WriteAudit("upload", "info", nil, "uploaded file", "", "", "")
	svc.WriteAudit("login", "info", nil, "logged in again", "", "", "")

	entries, total, _ := svc.QueryAuditLogs("login", "", nil, nil, nil, 50, 0)
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
}

func TestLogQueryAuditLogs_FilterByUserID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)
	userSvc := NewUserService(db)
	u1 := createTestUser(t, userSvc, "a@test.com", "A", "User")
	u2 := createTestUser(t, userSvc, "b@test.com", "B", "User")

	u1id := u1.ID
	u2id := u2.ID
	svc.WriteAudit("login", "info", &u1id, "u1 login", "", "", "")
	svc.WriteAudit("login", "info", &u2id, "u2 login", "", "", "")

	entries, _, _ := svc.QueryAuditLogs("", "", &u1id, nil, nil, 50, 0)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for u1, got %d", len(entries))
	}
}

func TestLogQueryAuditLogs_FilterByTime(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	svc.WriteAudit("login", "info", nil, "event", "", "", "")

	future := time.Now().Add(1 * time.Hour)
	entries, _, _ := svc.QueryAuditLogs("", "", nil, &future, nil, 50, 0)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries in future, got %d", len(entries))
	}
}

func TestLogQuerySystemLogs_FilterByLevel(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	svc.WriteSystem("info", "comp", "info msg", "", "")
	svc.WriteSystem("error", "comp", "error msg", "details", "")
	svc.WriteSystem("warn", "comp", "warn msg", "", "")

	entries, total, _ := svc.QuerySystemLogs("error", "", nil, nil, 50, 0)
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d, want 1", len(entries))
	}
}

func TestLogQuerySystemLogs_FilterByComponent(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	svc.WriteSystem("info", "uploader", "msg1", "", "")
	svc.WriteSystem("info", "auth", "msg2", "", "")

	entries, _, _ := svc.QuerySystemLogs("", "uploader", nil, nil, 50, 0)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestLogQuerySystemLogs_DefaultLimit(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	for i := 0; i < 60; i++ {
		svc.WriteSystem("info", "comp", "msg", "", "")
	}

	entries, _, _ := svc.QuerySystemLogs("", "", nil, nil, 0, 0)
	if len(entries) != 50 {
		t.Errorf("default limit should be 50, got %d", len(entries))
	}
}

func TestLogCleanupOldLogs(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)

	svc.WriteSystem("info", "comp", "msg1", "", "")
	svc.WriteSystem("info", "comp", "msg2", "", "")

	// Cleanup entries older than 1 day (fresh entries should not be deleted)
	deleted, err := svc.CleanupOldLogs(1)
	if err != nil {
		t.Fatalf("CleanupOldLogs: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted (entries are fresh), got %d", deleted)
	}
}

func TestLogQueryUserActivity(t *testing.T) {
	db := setupTestDB(t)
	svc := NewLogService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	uid := user.ID
	svc.WriteAudit("login", "info", &uid, "logged in", "", "", "")
	svc.WriteAudit("upload", "info", &uid, "uploaded file", "", "", "")

	entries, total, err := svc.QueryUserActivity("", &uid, nil, nil, 50, 0)
	if err != nil {
		t.Fatalf("QueryUserActivity: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
}
