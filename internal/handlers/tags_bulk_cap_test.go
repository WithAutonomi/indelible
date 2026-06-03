package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBulkTag_RejectsOversizedUUIDList verifies the explicit-UUID target list is
// capped so a single request can't fan out into unbounded per-UUID DB lookups.
func TestBulkTag_RejectsOversizedUUIDList(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	uuids := make([]string, 1001) // one over the 1000 cap
	for i := range uuids {
		uuids[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
	}
	body, _ := json.Marshal(map[string]any{
		"upload_uuids": uuids,
		"add_tags":     map[string][]string{"team": {"platform"}},
	})
	req := httptest.NewRequest("POST", "/api/v2/tags/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized bulk tag: got %d, want 400; body: %s", w.Code, w.Body.String())
	}
}
