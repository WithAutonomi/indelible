package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetNotificationPrefsDefault(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "user@test.com", "password123", "Test", "User")

	req := httptest.NewRequest("GET", "/api/v2/notifications/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get notification prefs: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Default should be realtime
	if resp["digest_mode"] != "realtime" {
		t.Errorf("digest_mode = %v, want realtime", resp["digest_mode"])
	}
}

func TestUpdateNotificationPrefs(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "user@test.com", "password123", "Test", "User")

	webhookURL := "https://example.com/notify"
	body, _ := json.Marshal(map[string]any{
		"webhook_url": webhookURL,
		"events":      `["completed"]`,
		"digest_mode": "daily",
	})
	req := httptest.NewRequest("PUT", "/api/v2/notifications/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update notification prefs: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["digest_mode"] != "daily" {
		t.Errorf("digest_mode = %v, want daily", resp["digest_mode"])
	}
	if resp["webhook_url"] != webhookURL {
		t.Errorf("webhook_url = %v, want %s", resp["webhook_url"], webhookURL)
	}
}

func TestUpdateNotificationPrefsInvalidDigest(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "user@test.com", "password123", "Test", "User")

	body, _ := json.Marshal(map[string]any{
		"digest_mode": "invalid",
	})
	req := httptest.NewRequest("PUT", "/api/v2/notifications/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid digest_mode, got %d", w.Code)
	}
}
