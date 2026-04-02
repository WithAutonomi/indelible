package services

import (
	"testing"
)

func TestNotificationPrefGet_Default(t *testing.T) {
	db := setupTestDB(t)
	svc := NewNotificationPrefService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	pref, err := svc.Get(user.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pref.UserID != user.ID {
		t.Errorf("UserID = %d", pref.UserID)
	}
	if pref.Events != "[]" {
		t.Errorf("default Events = %q, want '[]'", pref.Events)
	}
	if pref.DigestMode != "realtime" {
		t.Errorf("default DigestMode = %q, want 'realtime'", pref.DigestMode)
	}
	if pref.WebhookURL.Valid {
		t.Error("default WebhookURL should be null")
	}
}

func TestNotificationPrefUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewNotificationPrefService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	url := "https://example.com/notify"
	pref, err := svc.Update(user.ID, &url, `["completed","failed"]`, "daily")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !pref.WebhookURL.Valid || pref.WebhookURL.String != url {
		t.Errorf("WebhookURL = %v", pref.WebhookURL)
	}
	if pref.Events != `["completed","failed"]` {
		t.Errorf("Events = %q", pref.Events)
	}
	if pref.DigestMode != "daily" {
		t.Errorf("DigestMode = %q", pref.DigestMode)
	}
}

func TestNotificationPrefUpdate_Upsert(t *testing.T) {
	db := setupTestDB(t)
	svc := NewNotificationPrefService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	// First update creates
	url := "https://a.com"
	svc.Update(user.ID, &url, `["completed"]`, "realtime")

	// Second update modifies
	url2 := "https://b.com"
	pref, err := svc.Update(user.ID, &url2, `["failed"]`, "weekly")
	if err != nil {
		t.Fatalf("Update (upsert): %v", err)
	}
	if pref.WebhookURL.String != "https://b.com" {
		t.Errorf("WebhookURL = %q", pref.WebhookURL.String)
	}
	if pref.DigestMode != "weekly" {
		t.Errorf("DigestMode = %q", pref.DigestMode)
	}
}

func TestNotificationPrefUpdate_Defaults(t *testing.T) {
	db := setupTestDB(t)
	svc := NewNotificationPrefService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	// Pass empty values to get defaults
	pref, err := svc.Update(user.ID, nil, "", "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if pref.Events != "[]" {
		t.Errorf("Events = %q, want '[]'", pref.Events)
	}
	if pref.DigestMode != "realtime" {
		t.Errorf("DigestMode = %q, want 'realtime'", pref.DigestMode)
	}
}

func TestNotificationPrefGet_AfterUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewNotificationPrefService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	url := "https://hook.example.com"
	svc.Update(user.ID, &url, `["completed"]`, "daily")

	// Get should return persisted preferences (not defaults)
	pref, err := svc.Get(user.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pref.ID == 0 {
		t.Error("should have a real ID after update")
	}
	if pref.DigestMode != "daily" {
		t.Errorf("DigestMode = %q", pref.DigestMode)
	}
}
