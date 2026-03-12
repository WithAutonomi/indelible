package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// registerAndGetToken is a test helper that registers a user and returns the JWT.
func registerAndGetToken(t *testing.T, router http.Handler, email, password, first, last string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": password,
		"first_name": first, "last_name": last,
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register %s: got %d, body: %s", email, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["token"].(string)
}

func TestAdminListUsers(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list users: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	total := resp["total"].(float64)
	if total != 2 {
		t.Errorf("total = %v, want 2", total)
	}
	users := resp["users"].([]any)
	if len(users) != 2 {
		t.Errorf("users len = %d, want 2", len(users))
	}
}

func TestAdminCreateServiceAccount(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{
		"email":       "ci-bot@test.com",
		"first_name":  "CI Bot",
		"last_name":   "",
		"permissions": "write",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/users/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create service account: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["is_service_account"] != true {
		t.Error("expected is_service_account = true")
	}
	if resp["permissions"] != "write" {
		t.Errorf("permissions = %v, want write", resp["permissions"])
	}
}

func TestAdminSetPermissions_PreventLastAdminRemoval(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Try to downgrade the only admin to read
	body, _ := json.Marshal(map[string]string{"permission": "read"})
	req := httptest.NewRequest("PUT", "/api/v2/admin/users/1/permissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("remove last admin: got %d, want 409. Body: %s", w.Code, w.Body.String())
	}
}

func TestNonAdminCannotAccessAdminRoutes(t *testing.T) {
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin access: got %d, want 403", w.Code)
	}
}
