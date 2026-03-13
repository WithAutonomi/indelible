package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminCreateAndListWallets(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create a wallet
	body, _ := json.Marshal(map[string]string{
		"name":        "Main Wallet",
		"address":     "0xabc123def456",
		"private_key": "super-secret-private-key-for-testing",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create wallet: got %d, body: %s", w.Code, w.Body.String())
	}

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	wallet := createResp["wallet"].(map[string]any)

	if wallet["name"] != "Main Wallet" {
		t.Errorf("name = %v, want Main Wallet", wallet["name"])
	}
	if wallet["is_default"] != true {
		t.Errorf("first wallet should be default")
	}
	if wallet["address"] != "0xabc123def456" {
		t.Errorf("address = %v", wallet["address"])
	}

	// List wallets
	req = httptest.NewRequest("GET", "/api/v2/admin/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list wallets: got %d, body: %s", w.Code, w.Body.String())
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	wallets := listResp["wallets"].([]any)
	if len(wallets) != 1 {
		t.Errorf("expected 1 wallet, got %d", len(wallets))
	}
}

func TestAdminSetDefaultWallet(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create two wallets
	for _, name := range []string{"Wallet A", "Wallet B"} {
		body, _ := json.Marshal(map[string]string{
			"name":        name,
			"address":     "0x" + name,
			"private_key": "key-" + name,
		})
		req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create wallet %s: got %d", name, w.Code)
		}
	}

	// Set wallet 2 as default
	req := httptest.NewRequest("PUT", "/api/v2/admin/wallets/2/default", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("set default: got %d, body: %s", w.Code, w.Body.String())
	}

	// List and verify
	req = httptest.NewRequest("GET", "/api/v2/admin/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	wallets := resp["wallets"].([]any)

	for _, wl := range wallets {
		wm := wl.(map[string]any)
		id := wm["id"].(float64)
		isDefault := wm["is_default"].(bool)
		if id == 2 && !isDefault {
			t.Error("wallet 2 should be default")
		}
		if id == 1 && isDefault {
			t.Error("wallet 1 should no longer be default")
		}
	}
}

func TestAdminWalletsMissingFields(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{
		"name": "Incomplete",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("incomplete wallet: got %d, want 400", w.Code)
	}
}

func TestNonAdminCannotManageWallets(t *testing.T) {
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin wallet access: got %d, want 403", w.Code)
	}
}
