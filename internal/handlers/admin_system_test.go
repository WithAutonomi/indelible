package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The version-check endpoint must always return 200 with the expected shape,
// whether or not GitHub is reachable (it degrades gracefully). This test does
// not assert on the actual latest versions — only that the route is wired and
// the contract holds.
func TestAdminVersionCheck(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/version-check", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("version-check: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, k := range []string{"indelible", "antd", "github_reachable"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("missing key %q in response: %s", k, w.Body.String())
		}
	}
}

// The storage endpoint always returns 200 with the disk-usage shape. It must
// degrade gracefully (available=false) rather than error when figures can't be
// read, and omit the quota field when no system quota is configured.
func TestAdminStorage(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/storage", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("storage: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, k := range []string{"data_dir", "volume", "available", "total_bytes", "used_bytes", "free_bytes", "used_pct"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("missing key %q in response: %s", k, w.Body.String())
		}
	}
	// No system quota configured in the test fixture → field omitted.
	if _, ok := resp["quota"]; ok {
		t.Errorf("expected quota omitted when unset, got: %s", w.Body.String())
	}
}
