package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminWebhookCRUD(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create
	body, _ := json.Marshal(map[string]string{
		"url":    "https://example.com/hook",
		"events": `["completed","failed"]`,
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/webhooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create webhook: got %d, body: %s", w.Code, w.Body.String())
	}

	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	webhook := created["webhook"].(map[string]any)
	whID := webhook["id"].(float64)

	// List
	req = httptest.NewRequest("GET", "/api/v2/admin/webhooks", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list webhooks: got %d", w.Code)
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	webhooks := listResp["webhooks"].([]any)
	if len(webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(webhooks))
	}

	// Update
	updateBody, _ := json.Marshal(map[string]any{
		"url":              "https://example.com/hook-v2",
		"integration_type": "generic",
		"events":           `["completed"]`,
		"is_enabled":       false,
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/webhooks/%d", int64(whID)), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update webhook: got %d, body: %s", w.Code, w.Body.String())
	}

	// Delete
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/admin/webhooks/%d", int64(whID)), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete webhook: got %d", w.Code)
	}
}

func TestAdminWebhookDeadLetterQueue(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")

	// Create a webhook to own the dead-letter row, then seed a dead-letter
	// directly (forcing a real retry-exhaustion through the API isn't practical).
	createBody, _ := json.Marshal(map[string]string{"url": "https://example.com/hook"})
	req := httptest.NewRequest("POST", "/api/v2/admin/webhooks", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create webhook: got %d, body: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	whID := int64(created["webhook"].(map[string]any)["id"].(float64))

	if _, err := db.Exec(
		`INSERT INTO webhook_dead_letter (webhook_id, event_type, payload, last_status_code, last_error, attempts, is_auth) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		whID, "auth.password_reset_requested", `{"event_type":"auth.password_reset_requested","auth":{"to":"u@e.com","url":"https://secret-link"}}`, 500, "HTTP 500", 3, true,
	); err != nil {
		t.Fatalf("seed dead-letter: %v", err)
	}

	// List the queue.
	req = httptest.NewRequest("GET", "/api/v2/admin/webhooks/dead-letters", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list dead-letters: got %d, body: %s", w.Code, w.Body.String())
	}
	// The raw payload (which carries the one-time link) must never be exposed.
	if strings.Contains(w.Body.String(), "secret-link") {
		t.Fatal("dead-letter response leaked the stored payload")
	}
	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	entries := listResp["dead_letters"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 dead-letter, got %d", len(entries))
	}
	entry := entries[0].(map[string]any)
	if entry["is_auth"] != true {
		t.Errorf("is_auth = %v, want true", entry["is_auth"])
	}
	dlID := int64(entry["id"].(float64))

	// Resending a nonexistent entry → 404.
	req = httptest.NewRequest("POST", "/api/v2/admin/webhooks/dead-letters/99999/resend", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("resend missing: got %d, want 404", w.Code)
	}

	// Dismiss the seeded entry → 200, and it drops out of the unresolved queue.
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/admin/webhooks/dead-letters/%d", dlID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dismiss: got %d, body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/v2/admin/webhooks/dead-letters", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &listResp)
	if n := len(listResp["dead_letters"].([]any)); n != 0 {
		t.Errorf("expected 0 unresolved after dismiss, got %d", n)
	}
}

func TestAdminCreateWebhookRequiresURL(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("POST", "/api/v2/admin/webhooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing url, got %d", w.Code)
	}
}
