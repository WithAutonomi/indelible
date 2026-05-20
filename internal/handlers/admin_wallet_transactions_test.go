package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// V2-321: per-wallet transaction history surfacing. Service write path is
// already covered by services/transaction_test.go; this just verifies the
// new GET /admin/wallets/{id}/transactions handler is wired through chi,
// authed correctly, and returns the expected JSON shape.

func TestAdminWalletTransactions_ReturnsExpectedShape(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{
		"name":        "test-w",
		"private_key": "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create wallet: got %d, body: %s", w.Code, w.Body.String())
	}

	// GET the new endpoint. Fresh wallet → empty list with the right shape.
	req = httptest.NewRequest("GET", "/api/v2/admin/wallets/1/transactions", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list transactions: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["transactions"]; !ok {
		t.Errorf("response missing 'transactions' key: %s", w.Body.String())
	}
	if _, ok := resp["total"]; !ok {
		t.Errorf("response missing 'total' key: %s", w.Body.String())
	}
}

func TestAdminWalletTransactions_RequiresAdmin(t *testing.T) {
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password456", "User", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/wallets/1/transactions", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin access: got %d, want 403", w.Code)
	}
}
