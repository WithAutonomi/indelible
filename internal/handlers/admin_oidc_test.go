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

func TestAdminOIDCProvider_ExtraAuthorizeParamsRoundTrip(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create a provider, then PUT /extra-params to set hd=, then GET to confirm
	// the value survives across the wire in both directions.
	createBody, _ := json.Marshal(map[string]string{
		"name":          "google",
		"display_name":  "Google",
		"issuer_url":    "https://accounts.google.com",
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/oidc/providers", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	providerID := int64(created["id"].(float64))

	// Newly created — params should be present as an empty object, not null.
	if created["extra_authorize_params"] == nil {
		t.Errorf("extra_authorize_params should be an object on create response, got null")
	}

	// Set params via the partial-update endpoint.
	setBody, _ := json.Marshal(map[string]any{
		"extra_authorize_params": map[string]string{
			"hd":     "company.com",
			"prompt": "select_account",
		},
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/oidc/providers/%d/extra-params", providerID), bytes.NewReader(setBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set extra-params: %d %s", w.Code, w.Body.String())
	}

	// List should now echo the params back.
	req = httptest.NewRequest("GET", "/api/v2/admin/oidc/providers", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	providers := listResp["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	first := providers[0].(map[string]any)
	params, ok := first["extra_authorize_params"].(map[string]any)
	if !ok {
		t.Fatalf("extra_authorize_params not an object in list response: %T %v", first["extra_authorize_params"], first["extra_authorize_params"])
	}
	if params["hd"] != "company.com" {
		t.Errorf("hd = %v, want company.com", params["hd"])
	}
	if params["prompt"] != "select_account" {
		t.Errorf("prompt = %v, want select_account", params["prompt"])
	}

	// Empty-map clears.
	clearBody, _ := json.Marshal(map[string]any{
		"extra_authorize_params": map[string]string{},
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/oidc/providers/%d/extra-params", providerID), bytes.NewReader(clearBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear extra-params: %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/v2/admin/oidc/providers", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &listResp)
	cleared := listResp["providers"].([]any)[0].(map[string]any)
	if got := cleared["extra_authorize_params"].(map[string]any); len(got) != 0 {
		t.Errorf("expected empty params after clear, got %v", got)
	}
}

func TestAdminOIDCProvider_ExtraAuthorizeParams_RejectsEmptyKey(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	createBody, _ := json.Marshal(map[string]string{
		"name": "google", "display_name": "Google",
		"issuer_url": "https://accounts.google.com",
		"client_id":  "c", "client_secret": "s",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/oidc/providers", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	providerID := int64(created["id"].(float64))

	// Empty/whitespace-only key would produce "?=value" in the authorize URL.
	bad, _ := json.Marshal(map[string]any{
		"extra_authorize_params": map[string]string{"   ": "company.com"},
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/oidc/providers/%d/extra-params", providerID), bytes.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for blank key, got %d", w.Code)
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
