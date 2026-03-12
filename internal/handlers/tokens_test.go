package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAndListTokens(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create API token
	body, _ := json.Marshal(map[string]any{
		"name":        "CI Token",
		"description": "For CI pipeline",
		"permissions": []string{"read", "write"},
	})
	req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create token: got %d, body: %s", w.Code, w.Body.String())
	}

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)

	secret := createResp["secret"].(string)
	if secret == "" {
		t.Fatal("missing secret in create response")
	}
	if len(secret) < 10 {
		t.Errorf("secret too short: %s", secret)
	}

	tokenData := createResp["token"].(map[string]any)
	tokenUUID := tokenData["uuid"].(string)
	if tokenUUID == "" {
		t.Fatal("missing uuid")
	}
	if tokenData["name"] != "CI Token" {
		t.Errorf("name = %v, want CI Token", tokenData["name"])
	}

	// List tokens
	req = httptest.NewRequest("GET", "/api/v2/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list tokens: got %d, body: %s", w.Code, w.Body.String())
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	tokens := listResp["tokens"].([]any)
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}
}

func TestAPITokenAuthentication(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create API token
	body, _ := json.Marshal(map[string]any{
		"name":        "Auth Test Token",
		"permissions": []string{"read"},
	})
	req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	apiSecret := createResp["secret"].(string)

	// Use API token to access profile
	req = httptest.NewRequest("GET", "/api/v2/me", nil)
	req.Header.Set("Authorization", "Bearer "+apiSecret)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("API token auth: got %d, body: %s", w.Code, w.Body.String())
	}

	var profile map[string]any
	json.Unmarshal(w.Body.Bytes(), &profile)
	if profile["email"] != "admin@test.com" {
		t.Errorf("email = %v, want admin@test.com", profile["email"])
	}
}

func TestRevokeToken(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create token
	body, _ := json.Marshal(map[string]any{
		"name":        "Revoke Test",
		"permissions": []string{"read"},
	})
	req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	apiSecret := createResp["secret"].(string)
	tokenUUID := createResp["token"].(map[string]any)["uuid"].(string)

	// Revoke it
	revokeBody, _ := json.Marshal(map[string]string{"reason": "no longer needed"})
	req = httptest.NewRequest("DELETE", "/api/v2/tokens/"+tokenUUID, bytes.NewReader(revokeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("revoke: got %d, body: %s", w.Code, w.Body.String())
	}

	// Try to use the revoked token — should fail
	req = httptest.NewRequest("GET", "/api/v2/me", nil)
	req.Header.Set("Authorization", "Bearer "+apiSecret)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("revoked token access: got %d, want 401", w.Code)
	}
}

func TestNonAdminCannotCreateAdminToken(t *testing.T) {
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	body, _ := json.Marshal(map[string]any{
		"name":        "Sneaky Admin Token",
		"permissions": []string{"admin"},
	})
	req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin creating admin token: got %d, want 403", w.Code)
	}
}

func TestAdminListAllTokens(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create a couple tokens
	for _, name := range []string{"Token A", "Token B"} {
		body, _ := json.Marshal(map[string]any{"name": name, "permissions": []string{"read"}})
		req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Admin list
	req := httptest.NewRequest("GET", "/api/v2/admin/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("admin list: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	total := resp["total"].(float64)
	if total != 2 {
		t.Errorf("total = %v, want 2", total)
	}
}
