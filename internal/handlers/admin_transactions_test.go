package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The cross-wallet transactions endpoint (V2-447) returns the standard
// paginated shape and degrades to an empty list when there are no rows. It
// rejects a non-numeric wallet_id rather than silently ignoring it.
func TestAdminCrossWalletTransactions(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/transactions", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("transactions: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, k := range []string{"transactions", "total", "limit", "offset"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("missing key %q in response: %s", k, w.Body.String())
		}
	}

	// A non-numeric wallet_id is a client error, not a silent no-op.
	req = httptest.NewRequest("GET", "/api/v2/admin/transactions?wallet_id=abc", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid wallet_id: got %d, want 400", w.Code)
	}

	// Filters (type + date range) are accepted and still return the paged shape.
	req = httptest.NewRequest("GET", "/api/v2/admin/transactions?type=upload&from=2026-01-01&to=2026-12-31", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("filtered transactions: got %d, body: %s", w.Code, w.Body.String())
	}
}
