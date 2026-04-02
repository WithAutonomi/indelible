package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminUploadAnalytics(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload a file to have some data
	fileData := []byte("analytics test file")
	body, contentType := createMultipartUpload(t, "analytics.txt", fileData, "public")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Get upload analytics
	req = httptest.NewRequest("GET", "/api/v2/admin/analytics/uploads?days=30", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("upload analytics: got %d, body: %s", w.Code, w.Body.String())
	}

	var stats map[string]any
	json.Unmarshal(w.Body.Bytes(), &stats)

	totalUploads := stats["total_uploads"].(float64)
	if totalUploads != 1 {
		t.Errorf("total_uploads = %v, want 1", totalUploads)
	}

	statusCounts := stats["status_counts"].(map[string]any)
	if statusCounts["queued"].(float64) != 1 {
		t.Errorf("queued count = %v, want 1", statusCounts["queued"])
	}
}

func TestAdminCostAnalytics(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/analytics/costs", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("cost analytics: got %d, body: %s", w.Code, w.Body.String())
	}

	var stats map[string]any
	json.Unmarshal(w.Body.Bytes(), &stats)

	// With no completed uploads, totals should be zero
	if stats["total_uploads"].(float64) != 0 {
		t.Errorf("total_uploads = %v, want 0 (no completed uploads)", stats["total_uploads"])
	}
}

func TestAdminTokenAnalytics(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/analytics/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("token analytics: got %d, body: %s", w.Code, w.Body.String())
	}

	var stats map[string]any
	json.Unmarshal(w.Body.Bytes(), &stats)

	// No API token usage yet (we used JWT auth)
	if stats["total_requests"].(float64) != 0 {
		t.Errorf("total_requests = %v, want 0", stats["total_requests"])
	}
}
