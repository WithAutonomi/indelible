package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// V2-328 (auto-revoke on deactivation) + V2-329 (revoke_by + revoke_reason
// round-trip on the token response). Both ride on the same handler-test
// scaffolding so they share a file.

// createOwnedToken issues a token for the calling user and returns its UUID.
func createOwnedToken(t *testing.T, router http.Handler, ownerToken, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":        name,
		"permissions": []string{"read"},
	})
	req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create token %q: got %d, body: %s", name, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["token"].(map[string]any)["uuid"].(string)
}

// adminListTokens returns all tokens visible to the admin via /admin/tokens.
func adminListTokens(t *testing.T, router http.Handler, adminToken string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v2/admin/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin list tokens: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	raw := resp["tokens"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(map[string]any))
	}
	return out
}

func TestTokenResponse_EchoesRevokedByAndReason(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	tokenUUID := createOwnedToken(t, router, adminToken, "to-be-revoked")

	// Revoke with a reason.
	body, _ := json.Marshal(map[string]string{"reason": "rotation"})
	req := httptest.NewRequest("DELETE", "/api/v2/tokens/"+tokenUUID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: got %d, body: %s", w.Code, w.Body.String())
	}

	// Inspect via admin list.
	tokens := adminListTokens(t, router, adminToken)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok["revoke_reason"] != "rotation" {
		t.Errorf("revoke_reason = %v, want rotation", tok["revoke_reason"])
	}
	by, ok := tok["revoked_by"].(float64)
	if !ok || int64(by) != 1 {
		t.Errorf("revoked_by = %v, want 1 (admin)", tok["revoked_by"])
	}
}

func TestTokenLifecycle_DisableUserRevokesAllTheirTokens(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "victim@test.com", "password123", "Victim", "User")

	// User creates 3 tokens.
	for i := 0; i < 3; i++ {
		createOwnedToken(t, router, userToken, fmt.Sprintf("t%d", i))
	}

	// Admin disables the user. The user is id=2 (admin is id=1).
	body, _ := json.Marshal(map[string]any{"is_active": false})
	req := httptest.NewRequest("PUT", "/api/v2/admin/users/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin disable: got %d, body: %s", w.Code, w.Body.String())
	}

	// All three of the victim's tokens are now revoked with the right reason.
	tokens := adminListTokens(t, router, adminToken)
	victimRevoked := 0
	for _, tok := range tokens {
		if tok["owner_id"].(float64) != 2 {
			continue
		}
		if tok["revoked_at"] == nil {
			t.Errorf("token %v still active after disable", tok["uuid"])
			continue
		}
		if tok["revoke_reason"] != "user deactivated" {
			t.Errorf("token %v reason = %v, want %q", tok["uuid"], tok["revoke_reason"], "user deactivated")
		}
		if by, ok := tok["revoked_by"].(float64); !ok || int64(by) != 1 {
			t.Errorf("token %v revoked_by = %v, want 1 (admin)", tok["uuid"], tok["revoked_by"])
		}
		victimRevoked++
	}
	if victimRevoked != 3 {
		t.Errorf("victim token count = %d, want 3", victimRevoked)
	}
}

func TestTokenLifecycle_DeleteUserRevokesAllTheirTokens(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "leaver@test.com", "password123", "Leaving", "User")
	createOwnedToken(t, router, userToken, "ci-pipeline")

	req := httptest.NewRequest("DELETE", "/api/v2/admin/users/2", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin delete: got %d, body: %s", w.Code, w.Body.String())
	}

	for _, tok := range adminListTokens(t, router, adminToken) {
		if tok["owner_id"].(float64) != 2 {
			continue
		}
		if tok["revoked_at"] == nil {
			t.Errorf("token %v still active after delete", tok["uuid"])
		}
		if tok["revoke_reason"] != "user deleted" {
			t.Errorf("token %v reason = %v, want %q", tok["uuid"], tok["revoke_reason"], "user deleted")
		}
	}
}

func TestTokenLifecycle_ReactivatingUserDoesNotUnrevokeTokens(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "yo-yo@test.com", "password123", "Yo", "Yo")
	createOwnedToken(t, router, userToken, "before-disable")

	// Disable.
	body, _ := json.Marshal(map[string]any{"is_active": false})
	req := httptest.NewRequest("PUT", "/api/v2/admin/users/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin disable: got %d, body: %s", w.Code, w.Body.String())
	}

	// Re-enable.
	body, _ = json.Marshal(map[string]any{"is_active": true})
	req = httptest.NewRequest("PUT", "/api/v2/admin/users/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin re-enable: got %d, body: %s", w.Code, w.Body.String())
	}

	// Original token is still revoked.
	for _, tok := range adminListTokens(t, router, adminToken) {
		if tok["owner_id"].(float64) == 2 && tok["revoked_at"] == nil {
			t.Errorf("token %v became active again after re-enable — should stay revoked", tok["uuid"])
		}
	}
}

func TestTokenLifecycle_DisableNoOpWhenAlreadyInactive(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "User", "Two")
	tokUUID := createOwnedToken(t, router, userToken, "active-token")

	// Disable once.
	body, _ := json.Marshal(map[string]any{"is_active": false})
	req := httptest.NewRequest("PUT", "/api/v2/admin/users/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first disable: got %d, body: %s", w.Code, w.Body.String())
	}

	// Capture revoke timestamp.
	tokensBefore := adminListTokens(t, router, adminToken)
	var revokedAtBefore any
	for _, tok := range tokensBefore {
		if tok["uuid"] == tokUUID {
			revokedAtBefore = tok["revoked_at"]
		}
	}
	if revokedAtBefore == nil {
		t.Fatal("token was not revoked on first disable")
	}

	// Second PUT with is_active=false should not re-revoke (no transition).
	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/api/v2/admin/users/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second disable: got %d, body: %s", w.Code, w.Body.String())
	}

	for _, tok := range adminListTokens(t, router, adminToken) {
		if tok["uuid"] == tokUUID && tok["revoked_at"] != revokedAtBefore {
			t.Errorf("token revoke_at changed on no-op disable: was %v, now %v", revokedAtBefore, tok["revoked_at"])
		}
	}
}
