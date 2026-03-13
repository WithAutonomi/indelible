package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminGetSettings(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/settings", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get settings: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	settings := resp["settings"].(map[string]any)

	// Should have seeded defaults
	if settings["max_upload_size_bytes"] != "10737418240" {
		t.Errorf("max_upload_size_bytes = %v", settings["max_upload_size_bytes"])
	}
}

func TestAdminUpdateSettings(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Update a setting
	body, _ := json.Marshal(map[string]string{
		"environment_name": "staging",
		"timezone":         "America/New_York",
	})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update settings: got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify
	req = httptest.NewRequest("GET", "/api/v2/admin/settings", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	settings := resp["settings"].(map[string]any)

	if settings["environment_name"] != "staging" {
		t.Errorf("environment_name = %v, want staging", settings["environment_name"])
	}
	if settings["timezone"] != "America/New_York" {
		t.Errorf("timezone = %v", settings["timezone"])
	}
}

func TestAdminExportImportSettings(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Export
	req := httptest.NewRequest("GET", "/api/v2/admin/settings/export", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("export: got %d", w.Code)
	}

	// Import with modified value
	var exported map[string]string
	json.Unmarshal(w.Body.Bytes(), &exported)
	exported["environment_name"] = "imported-env"

	importBody, _ := json.Marshal(exported)
	req = httptest.NewRequest("POST", "/api/v2/admin/settings/import", bytes.NewReader(importBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("import: got %d, body: %s", w.Code, w.Body.String())
	}
}
