package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminOIDCProviderCRUD(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create
	body, _ := json.Marshal(map[string]string{
		"name":          "google",
		"display_name":  "Google",
		"issuer_url":    "https://accounts.google.com",
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"scopes":        "openid email profile",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/oidc/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create OIDC provider: got %d, body: %s", w.Code, w.Body.String())
	}

	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	providerID := created["id"].(float64)

	if created["name"] != "google" {
		t.Errorf("name = %v, want google", created["name"])
	}

	// List
	req = httptest.NewRequest("GET", "/api/v2/admin/oidc/providers", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list OIDC providers: got %d", w.Code)
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	providers := listResp["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}

	// Update
	updateBody, _ := json.Marshal(map[string]any{
		"name":         "google",
		"display_name": "Google SSO",
		"issuer_url":   "https://accounts.google.com",
		"client_id":    "test-client-id",
		"scopes":       "openid email profile",
		"is_enabled":   true,
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/oidc/providers/%d", int64(providerID)), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update OIDC provider: got %d, body: %s", w.Code, w.Body.String())
	}

	var updated map[string]any
	json.Unmarshal(w.Body.Bytes(), &updated)
	if updated["display_name"] != "Google SSO" {
		t.Errorf("display_name = %v, want Google SSO", updated["display_name"])
	}

	// Delete
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/admin/oidc/providers/%d", int64(providerID)), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete OIDC provider: got %d", w.Code)
	}
}

func TestAdminCreateOIDCProviderValidation(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Missing required fields
	body, _ := json.Marshal(map[string]string{
		"name": "incomplete",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/oidc/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for incomplete OIDC provider, got %d", w.Code)
	}
}
