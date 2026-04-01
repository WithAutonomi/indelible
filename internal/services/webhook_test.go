package services

import (
	"strings"
	"testing"
)

func TestWebhookCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh, err := svc.Create("https://example.com/hook", "generic", `["completed"]`)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wh.URL != "https://example.com/hook" {
		t.Errorf("URL = %q", wh.URL)
	}
	if wh.IntegrationType != "generic" {
		t.Errorf("IntegrationType = %q", wh.IntegrationType)
	}
	if !wh.IsEnabled {
		t.Error("new webhook should be enabled by default")
	}
}

func TestWebhookCreate_SecretPrefix(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh, err := svc.Create("https://example.com/hook", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(wh.Secret, "whsec_") {
		t.Errorf("secret should start with whsec_, got %q", wh.Secret)
	}
	// whsec_ + 64 hex chars = 70 total
	if len(wh.Secret) != 70 {
		t.Errorf("secret length = %d, want 70", len(wh.Secret))
	}
}

func TestWebhookCreate_Defaults(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh, _ := svc.Create("https://example.com/hook", "", "")
	if wh.IntegrationType != "generic" {
		t.Errorf("default integration_type = %q, want generic", wh.IntegrationType)
	}
	if wh.Events != `["completed","failed"]` {
		t.Errorf("default events = %q", wh.Events)
	}
}

func TestWebhookGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	created, _ := svc.Create("https://example.com", "", "")
	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch")
	}
}

func TestWebhookGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	_, err := svc.GetByID(999)
	if err != ErrWebhookNotFound {
		t.Errorf("expected ErrWebhookNotFound, got %v", err)
	}
}

func TestWebhookList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	svc.Create("https://a.com", "", "")
	svc.Create("https://b.com", "", "")

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestWebhookUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh, _ := svc.Create("https://old.com", "generic", `["completed"]`)

	updated, err := svc.Update(wh.ID, "https://new.com", "slack", `["completed","failed"]`, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.URL != "https://new.com" {
		t.Errorf("URL = %q", updated.URL)
	}
	if updated.IntegrationType != "slack" {
		t.Errorf("IntegrationType = %q", updated.IntegrationType)
	}
	if updated.IsEnabled {
		t.Error("should be disabled after update")
	}
}

func TestWebhookDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh, _ := svc.Create("https://example.com", "", "")

	if err := svc.Delete(wh.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := svc.GetByID(wh.ID)
	if err != ErrWebhookNotFound {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestWebhookDelete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	err := svc.Delete(999)
	if err != ErrWebhookNotFound {
		t.Errorf("expected ErrWebhookNotFound, got %v", err)
	}
}

func TestWebhookRotateSecret(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh, _ := svc.Create("https://example.com", "", "")
	oldSecret := wh.Secret

	newSecret, err := svc.RotateSecret(wh.ID)
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if !strings.HasPrefix(newSecret, "whsec_") {
		t.Errorf("rotated secret missing prefix: %q", newSecret)
	}
	if newSecret == oldSecret {
		t.Error("rotated secret should differ from old")
	}

	// Verify persisted
	got, _ := svc.GetByID(wh.ID)
	if got.Secret != newSecret {
		t.Error("persisted secret does not match rotated")
	}
}

func TestWebhookRotateSecret_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	_, err := svc.RotateSecret(999)
	if err != ErrWebhookNotFound {
		t.Errorf("expected ErrWebhookNotFound, got %v", err)
	}
}

func TestWebhookGetEnabled(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWebhookService(db)

	wh1, _ := svc.Create("https://a.com", "", "")
	svc.Create("https://b.com", "", "")

	// Disable first one
	svc.Update(wh1.ID, wh1.URL, wh1.IntegrationType, wh1.Events, false)

	enabled, err := svc.GetEnabled()
	if err != nil {
		t.Fatalf("GetEnabled: %v", err)
	}
	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled, got %d", len(enabled))
	}
}
