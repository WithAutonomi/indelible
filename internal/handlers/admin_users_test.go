package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// registerAndGetToken is a test helper that returns a JWT for the given user.
// It registers them; if the account already exists (e.g. the pre-seeded
// bootstrap admin — see setupTestRouter), it logs in instead. This keeps the
// long-standing convention that the first call with admin@test.com yields an
// admin token, while other emails register as ordinary read users.
func registerAndGetToken(t *testing.T, router http.Handler, email, password, first, last string) string {
	t.Helper()
	// The bootstrap admin is pre-seeded (see setupTestRouter) — log in directly
	// rather than attempting a register that 409s. Beyond saving a round-trip,
	// this avoids a Postgres-only footgun: a failed INSERT still consumes the
	// users id sequence, shifting every subsequent user id and breaking tests
	// that assert specific ids (SQLite reclaims the rowid, so it masks this).
	if email == seedAdminEmail {
		return loginAndGetToken(t, router, email, password)
	}

	body, _ := json.Marshal(map[string]string{
		"email": email, "password": password,
		"first_name": first, "last_name": last,
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	switch w.Code {
	case http.StatusCreated:
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["token"].(string)
	case http.StatusConflict:
		// Pre-seeded user (bootstrap admin). Log in to obtain the token.
		return loginAndGetToken(t, router, email, password)
	default:
		t.Fatalf("register %s: got %d, body: %s", email, w.Code, w.Body.String())
		return ""
	}
}

// loginAndGetToken logs in an existing user and returns the JWT.
func loginAndGetToken(t *testing.T, router http.Handler, email, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login %s: got %d, body: %s", email, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["token"].(string)
}

// createTestWallet is a test helper that creates a wallet via the admin API.
// The upload handler requires a default wallet to exist before accepting files.
func createTestWallet(t *testing.T, router http.Handler, adminToken string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"name":        "test-wallet",
		"private_key": "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create test wallet: got %d, body: %s", w.Code, w.Body.String())
	}
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
