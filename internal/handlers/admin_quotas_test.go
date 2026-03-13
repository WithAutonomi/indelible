package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminQuotaCRUD(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create system quota
	body, _ := json.Marshal(map[string]any{
		"entity_type": "system",
		"max_bytes":   1073741824, // 1GB
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/quotas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create quota: got %d, body: %s", w.Code, w.Body.String())
	}

	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	quotaID := created["id"].(float64)

	// List
	req = httptest.NewRequest("GET", "/api/v2/admin/quotas", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list quotas: got %d", w.Code)
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	quotas := listResp["quotas"].([]any)
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(quotas))
	}

	// Update
	updateBody, _ := json.Marshal(map[string]any{
		"max_bytes":  2147483648, // 2GB
		"is_enabled": true,
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/quotas/%d", int64(quotaID)), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update quota: got %d, body: %s", w.Code, w.Body.String())
	}

	// Delete
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/admin/quotas/%d", int64(quotaID)), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete quota: got %d", w.Code)
	}
}

func TestAdminCreateQuotaValidation(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Missing entity_type
	body, _ := json.Marshal(map[string]any{
		"max_bytes": 1024,
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/quotas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	// Zero max_bytes
	body, _ = json.Marshal(map[string]any{
		"entity_type": "system",
		"max_bytes":   0,
	})
	req = httptest.NewRequest("POST", "/api/v2/admin/quotas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero max_bytes, got %d", w.Code)
	}
}
