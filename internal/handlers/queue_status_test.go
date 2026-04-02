package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueueStatus_EmptyQueue(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/system/queue-status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("queue status: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if queued := resp["queued"].(float64); queued != 0 {
		t.Errorf("queued = %v, want 0", queued)
	}
	if processing := resp["processing"].(float64); processing != 0 {
		t.Errorf("processing = %v, want 0", processing)
	}
	if maxQueued := resp["max_queued"].(float64); maxQueued != 500 {
		t.Errorf("max_queued = %v, want 500", maxQueued)
	}
	if maxConcurrent := resp["max_concurrent"].(float64); maxConcurrent != 4 {
		t.Errorf("max_concurrent = %v, want 4", maxConcurrent)
	}
}

func TestQueueStatus_WithUploads(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload a file so there is at least one queued entry.
	fileData := []byte("queue status test file content")
	body, contentType := createMultipartUpload(t, "queue-test.txt", fileData, "public")

	uploadReq := httptest.NewRequest("POST", "/api/v2/uploads", body)
	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq.Header.Set("Authorization", "Bearer "+adminToken)
	uw := httptest.NewRecorder()
	router.ServeHTTP(uw, uploadReq)

	if uw.Code != http.StatusAccepted {
		t.Fatalf("upload: got %d, body: %s", uw.Code, uw.Body.String())
	}

	// Now check queue status.
	req := httptest.NewRequest("GET", "/api/v2/system/queue-status", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("queue status: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	queued := resp["queued"].(float64)
	if queued < 1 {
		t.Errorf("queued = %v, want >= 1 after upload", queued)
	}
}

func TestQueueStatus_Unauthenticated(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/v2/system/queue-status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated queue status: got %d, want 401", w.Code)
	}
}
