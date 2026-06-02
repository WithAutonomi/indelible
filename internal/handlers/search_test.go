package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doSearch(t *testing.T, router http.Handler, token, query string) map[string][]map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v2/search?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string][]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp
}

// The admin categories (users/tokens/webhooks) must be unreachable by a
// non-admin even when they request scope=all — enforced server-side, not just
// hidden in the UI.
func TestGlobalSearchAdminScopeEnforced(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "normal@test.com", "password123", "Normal", "User")

	// Admin + scope=all: the seeded "admin@test.com" user matches q=admin.
	adminResp := doSearch(t, router, adminToken, "q=admin&scope=all")
	if len(adminResp["users"]) == 0 {
		t.Error("admin scope=all: expected user matches for q=admin, got none")
	}

	// Non-admin + scope=all: admin categories must come back empty.
	userResp := doSearch(t, router, userToken, "q=admin&scope=all")
	for _, g := range []string{"users", "tokens", "webhooks"} {
		if len(userResp[g]) != 0 {
			t.Errorf("non-admin scope=all: group %s must be empty, got %d", g, len(userResp[g]))
		}
	}
}

// scope=all is opt-in: an admin who doesn't ask for it gets only the
// caller-scoped categories.
func TestGlobalSearchAdminScopeOptIn(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	resp := doSearch(t, router, adminToken, "q=admin")
	if len(resp["users"]) != 0 {
		t.Errorf("admin without scope=all: users should be empty, got %d", len(resp["users"]))
	}
}

// A sub-minimum query scans nothing.
func TestGlobalSearchShortQuery(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	resp := doSearch(t, router, token, "q=a&scope=all")
	for _, g := range []string{"files", "collections", "tags", "users", "tokens", "webhooks"} {
		if len(resp[g]) != 0 {
			t.Errorf("short query: group %s should be empty, got %d", g, len(resp[g]))
		}
	}
}
