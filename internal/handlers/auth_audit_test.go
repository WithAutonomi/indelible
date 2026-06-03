package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// V2-314: verify each auth/identity/token handler writes the expected audit
// row when it runs. Today the audit_log table is empty save for SCIM events;
// this guards the wiring so we don't lose coverage on a future refactor.

// countAuditEvents queries /admin/logs/audit with an event_type filter and
// returns the total. The admin token is the first registered user.
func countAuditEvents(t *testing.T, router http.Handler, adminToken, eventType string) int {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit?event_type="+eventType, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query audit logs (%s): got %d, body: %s", eventType, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return int(resp["total"].(float64))
}

func TestAudit_LoginSuccessAndFailure(t *testing.T) {
	router, db := setupRouterWithDB(t)
	// Clean audit slate — acquiring the admin token logs in (one audit row),
	// which would otherwise inflate the login count asserted below.
	adminToken := adminTokenCleanAudit(t, router, db)

	// Successful login.
	body, _ := json.Marshal(map[string]string{"email": "admin@test.com", "password": "password123"})
	req := httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: got %d, body: %s", w.Code, w.Body.String())
	}

	if n := countAuditEvents(t, router, adminToken, "login"); n != 1 {
		t.Errorf("login audit count = %d, want 1", n)
	}

	// Wrong password → login_failed with user_id resolved.
	body, _ = json.Marshal(map[string]string{"email": "admin@test.com", "password": "WRONG"})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password login: got %d", w.Code)
	}

	// Unknown email → login_failed with nil user_id.
	body, _ = json.Marshal(map[string]string{"email": "nobody@test.com", "password": "whatever"})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if n := countAuditEvents(t, router, adminToken, "login_failed"); n != 2 {
		t.Errorf("login_failed audit count = %d, want 2", n)
	}
}

func TestAudit_PasswordChange(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{
		"current_password": "password123",
		"new_password":     "newpassword456",
	})
	req := httptest.NewRequest("PUT", "/api/v2/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("change password: got %d, body: %s", w.Code, w.Body.String())
	}

	// Password change invalidates all sessions, so re-login to query audit.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "admin@test.com",
		"password": "newpassword456",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("re-login: got %d, body: %s", w.Code, w.Body.String())
	}
	var loginResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	freshToken := loginResp["token"].(string)

	if n := countAuditEvents(t, router, freshToken, "password_changed"); n != 1 {
		t.Errorf("password_changed audit count = %d, want 1", n)
	}
}

func TestAudit_PasswordResetRequested(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{"email": "admin@test.com"})
	req := httptest.NewRequest("POST", "/api/v2/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("forgot-password: got %d, body: %s", w.Code, w.Body.String())
	}

	if n := countAuditEvents(t, router, adminToken, "password_reset_requested"); n != 1 {
		t.Errorf("password_reset_requested audit count = %d, want 1", n)
	}

	// Unknown email still writes an audit row (warn severity) so brute-force
	// detection works even though we don't reveal email existence.
	body, _ = json.Marshal(map[string]string{"email": "nobody@test.com"})
	req = httptest.NewRequest("POST", "/api/v2/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if n := countAuditEvents(t, router, adminToken, "password_reset_requested"); n != 2 {
		t.Errorf("password_reset_requested (with unknown email) = %d, want 2", n)
	}
}

func TestAudit_TokenIssueAndRevoke(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Issue.
	body, _ := json.Marshal(map[string]any{
		"name":        "audit-test",
		"permissions": []string{"read"},
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
	tokenUUID := createResp["token"].(map[string]any)["uuid"].(string)

	// Revoke.
	revokeBody, _ := json.Marshal(map[string]string{"reason": "rotation"})
	req = httptest.NewRequest("DELETE", "/api/v2/tokens/"+tokenUUID, bytes.NewReader(revokeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke token: got %d, body: %s", w.Code, w.Body.String())
	}

	if n := countAuditEvents(t, router, adminToken, "api_token_issued"); n != 1 {
		t.Errorf("api_token_issued audit count = %d, want 1", n)
	}
	if n := countAuditEvents(t, router, adminToken, "api_token_revoked"); n != 1 {
		t.Errorf("api_token_revoked audit count = %d, want 1", n)
	}

	// Verify secret is NOT in the audit detail (would be a P0 leak).
	secret := createResp["secret"].(string)
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/audit?event_type=api_token_issued", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), secret) {
		t.Errorf("api_token_issued audit row leaked the raw secret %q", secret)
	}
}
