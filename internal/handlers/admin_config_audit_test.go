package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// V2-316: surface the config_audit table that SettingsService.Update
// already populates. End-to-end test: change a setting → query
// /admin/logs/config → assert old/new captured.

func TestConfigAudit_PatchSettingsLandsInConfigAudit(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// PATCH a setting.
	body, _ := json.Marshal(map[string]string{"scim_enabled": "true"})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch settings: got %d, body: %s", w.Code, w.Body.String())
	}

	// Query the new config_audit endpoint.
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/config", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query config_audit: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	entries := resp["entries"].([]any)
	if len(entries) == 0 {
		t.Fatal("config_audit table empty after settings patch")
	}

	// Find the scim_enabled entry.
	var found map[string]any
	for _, e := range entries {
		row := e.(map[string]any)
		if row["setting_key"] == "scim_enabled" {
			found = row
			break
		}
	}
	if found == nil {
		t.Fatalf("scim_enabled row not in config_audit; got: %s", w.Body.String())
	}
	if found["new_value"] != "true" {
		t.Errorf("new_value = %v, want \"true\"", found["new_value"])
	}
	by, ok := found["changed_by"].(float64)
	if !ok || int64(by) != 1 {
		t.Errorf("changed_by = %v, want 1 (admin)", found["changed_by"])
	}
}

func TestConfigAudit_FilterBySettingKey(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// PATCH two different keys.
	body, _ := json.Marshal(map[string]string{
		"scim_enabled":              "true",
		"default_token_expiry_days": "60",
	})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch settings: got %d", w.Code)
	}

	// Filter by setting_key=scim_enabled.
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/config?setting_key=scim_enabled", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered query: got %d", w.Code)
	}

	body2 := w.Body.String()
	if strings.Contains(body2, "default_token_expiry_days") {
		t.Errorf("filter setting_key=scim_enabled leaked other keys: %s", body2)
	}
	if !strings.Contains(body2, "scim_enabled") {
		t.Errorf("filter setting_key=scim_enabled didn't return the row: %s", body2)
	}
}
