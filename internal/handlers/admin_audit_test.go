package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// V2-315: every admin-surface handler that mutates state must land an audit
// row. These tests are deliberately minimal — one happy-path per handler —
// since the wiring is the only thing that can drift.

func TestAdminAudit_UserLifecycle(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create.
	body, _ := json.Marshal(map[string]string{
		"email": "victim@test.com", "password": "password456",
		"first_name": "V", "last_name": "Test", "permissions": "read",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: got %d, body: %s", w.Code, w.Body.String())
	}

	// Disable.
	body, _ = json.Marshal(map[string]any{"is_active": false})
	req = httptest.NewRequest("PUT", "/api/v2/admin/users/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable user: got %d", w.Code)
	}

	// Delete.
	req = httptest.NewRequest("DELETE", "/api/v2/admin/users/2", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete user: got %d", w.Code)
	}

	cases := map[string]int{"user_created": 1, "user_disabled": 1, "user_deleted": 1}
	for event, want := range cases {
		if got := countAuditEvents(t, router, adminToken, event); got != want {
			t.Errorf("%s audit count = %d, want %d", event, got, want)
		}
	}
}

func TestAdminAudit_GroupLifecycle(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	registerAndGetToken(t, router, "member@test.com", "password456", "M", "M")

	// Create group.
	body, _ := json.Marshal(map[string]any{"name": "engineering", "permission_level": "read"})
	req := httptest.NewRequest("POST", "/api/v2/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create group: got %d, body: %s", w.Code, w.Body.String())
	}

	// Add member (user id=2 = member@).
	body, _ = json.Marshal(map[string]int64{"user_id": 2})
	req = httptest.NewRequest("POST", "/api/v2/admin/groups/1/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: got %d, body: %s", w.Code, w.Body.String())
	}

	// Remove member.
	req = httptest.NewRequest("DELETE", "/api/v2/admin/groups/1/members/2", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remove member: got %d", w.Code)
	}

	// Delete group.
	req = httptest.NewRequest("DELETE", "/api/v2/admin/groups/1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete group: got %d", w.Code)
	}

	cases := map[string]int{
		"group_created":        1,
		"group_member_added":   1,
		"group_member_removed": 1,
		"group_deleted":        1,
	}
	for event, want := range cases {
		if got := countAuditEvents(t, router, adminToken, event); got != want {
			t.Errorf("%s audit count = %d, want %d", event, got, want)
		}
	}
}

func TestAdminAudit_WalletLifecycleAndAddressIsLogged(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create wallet — fixed devnet key.
	privKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	body, _ := json.Marshal(map[string]string{"name": "test-w", "private_key": privKey})
	req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create wallet: got %d, body: %s", w.Code, w.Body.String())
	}

	if got := countAuditEvents(t, router, adminToken, "wallet_created"); got != 1 {
		t.Errorf("wallet_created audit count = %d, want 1", got)
	}

	// Verify the audit row contains the PUBLIC ADDRESS but NEVER the private key.
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/audit?event_type=wallet_created", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	respBody := w.Body.String()
	if strings.Contains(respBody, privKey) {
		t.Errorf("wallet_created audit row LEAKED the private key: %s", privKey)
	}
	if !strings.Contains(respBody, "address=0x") {
		t.Errorf("wallet_created audit row missing public address: %s", respBody)
	}
}

func TestAdminAudit_ScimTokenIssueAndRevokeDoesNotLeakSecret(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Enable SCIM first.
	body, _ := json.Marshal(map[string]string{"scim_enabled": "true"})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable scim: got %d", w.Code)
	}

	// Create SCIM token.
	body, _ = json.Marshal(map[string]string{"name": "okta"})
	req = httptest.NewRequest("POST", "/api/v2/admin/scim/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create scim token: got %d, body: %s", w.Code, w.Body.String())
	}
	var tokResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &tokResp)
	secret := tokResp["secret"].(string)
	tokenID := int64(tokResp["token"].(map[string]any)["id"].(float64))

	// Verify audit row exists AND doesn't leak the secret.
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/audit?event_type=scim_token_issued", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	respBody := w.Body.String()
	if strings.Contains(respBody, secret) {
		t.Errorf("scim_token_issued audit row LEAKED the secret %q", secret)
	}

	// Revoke.
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/admin/scim/tokens/%d", tokenID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke scim token: got %d", w.Code)
	}

	if got := countAuditEvents(t, router, adminToken, "scim_token_revoked"); got != 1 {
		t.Errorf("scim_token_revoked audit count = %d, want 1", got)
	}
}

func TestAdminAudit_SettingsExportImport(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Export.
	req := httptest.NewRequest("GET", "/api/v2/admin/settings/export", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export settings: got %d", w.Code)
	}
	exportPayload := w.Body.Bytes()

	if got := countAuditEvents(t, router, adminToken, "settings_exported"); got != 1 {
		t.Errorf("settings_exported audit count = %d, want 1", got)
	}

	// Import the same export back.
	req = httptest.NewRequest("POST", "/api/v2/admin/settings/import", bytes.NewReader(exportPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import settings: got %d, body: %s", w.Code, w.Body.String())
	}

	if got := countAuditEvents(t, router, adminToken, "settings_imported"); got != 1 {
		t.Errorf("settings_imported audit count = %d, want 1", got)
	}
}
