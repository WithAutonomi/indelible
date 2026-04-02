package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminTagRuleCRUD(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// --- Create a rule ---
	createBody, _ := json.Marshal(map[string]any{
		"name":        "Mark PDFs",
		"description": "Auto-tag PDF uploads",
		"match_field": "content_type",
		"match_op":    "equals",
		"match_value": "application/pdf",
		"apply_key":   "filetype",
		"apply_value":  "pdf",
		"priority":    10,
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/tag-rules", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create rule: got %d, body: %s", w.Code, w.Body.String())
	}

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	rule := createResp["rule"].(map[string]any)
	ruleID := rule["id"].(float64)
	if rule["name"] != "Mark PDFs" {
		t.Errorf("name = %v, want Mark PDFs", rule["name"])
	}
	if rule["match_field"] != "content_type" {
		t.Errorf("match_field = %v, want content_type", rule["match_field"])
	}
	if rule["apply_key"] != "filetype" {
		t.Errorf("apply_key = %v, want filetype", rule["apply_key"])
	}
	if rule["is_enabled"] != true {
		t.Errorf("is_enabled = %v, want true", rule["is_enabled"])
	}

	// --- List rules ---
	req = httptest.NewRequest("GET", "/api/v2/admin/tag-rules", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list rules: got %d, body: %s", w.Code, w.Body.String())
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	rules := listResp["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	listedRule := rules[0].(map[string]any)
	if listedRule["name"] != "Mark PDFs" {
		t.Errorf("listed rule name = %v, want Mark PDFs", listedRule["name"])
	}

	// --- Update the rule ---
	isEnabled := false
	updateBody, _ := json.Marshal(map[string]any{
		"name":        "Mark PDFs v2",
		"description": "Updated description",
		"match_field": "content_type",
		"match_op":    "contains",
		"match_value": "pdf",
		"apply_key":   "filetype",
		"apply_value":  "document",
		"priority":    20,
		"is_enabled":  isEnabled,
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/tag-rules/%.0f", ruleID), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update rule: got %d, body: %s", w.Code, w.Body.String())
	}

	var updateResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &updateResp)
	updated := updateResp["rule"].(map[string]any)
	if updated["name"] != "Mark PDFs v2" {
		t.Errorf("updated name = %v, want Mark PDFs v2", updated["name"])
	}
	if updated["apply_value"] != "document" {
		t.Errorf("updated apply_value = %v, want document", updated["apply_value"])
	}
	if updated["is_enabled"] != false {
		t.Errorf("updated is_enabled = %v, want false", updated["is_enabled"])
	}

	// --- Delete the rule ---
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/admin/tag-rules/%.0f", ruleID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete rule: got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	req = httptest.NewRequest("GET", "/api/v2/admin/tag-rules", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	json.Unmarshal(w.Body.Bytes(), &listResp)
	rules = listResp["rules"].([]any)
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestAdminTagRuleValidation(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Invalid match_field should return 400
	body, _ := json.Marshal(map[string]any{
		"name":        "Bad Rule",
		"match_field": "invalid_field",
		"match_op":    "equals",
		"match_value": "test",
		"apply_key":   "tag1",
		"apply_value":  "val1",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/tag-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid match_field: got %d, want 400. Body: %s", w.Code, w.Body.String())
	}

	// Invalid match_op for the given field should return 400
	body, _ = json.Marshal(map[string]any{
		"name":        "Bad Op Rule",
		"match_field": "visibility",
		"match_op":    "regex",
		"match_value": "public",
		"apply_key":   "vis",
		"apply_value":  "yes",
	})
	req = httptest.NewRequest("POST", "/api/v2/admin/tag-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid match_op: got %d, want 400. Body: %s", w.Code, w.Body.String())
	}

	// Missing required fields should return 400
	body, _ = json.Marshal(map[string]any{
		"name": "Incomplete Rule",
	})
	req = httptest.NewRequest("POST", "/api/v2/admin/tag-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("missing fields: got %d, want 400. Body: %s", w.Code, w.Body.String())
	}
}

func TestNonAdminCannotManageTagRules(t *testing.T) {
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	// Try to list rules as non-admin
	req := httptest.NewRequest("GET", "/api/v2/admin/tag-rules", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin list rules: got %d, want 403", w.Code)
	}

	// Try to create a rule as non-admin
	body, _ := json.Marshal(map[string]any{
		"name":        "Sneaky Rule",
		"match_field": "filename",
		"match_op":    "contains",
		"match_value": ".exe",
		"apply_key":   "danger",
		"apply_value":  "true",
	})
	req = httptest.NewRequest("POST", "/api/v2/admin/tag-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin create rule: got %d, want 403", w.Code)
	}

	// Try to delete a rule as non-admin
	req = httptest.NewRequest("DELETE", "/api/v2/admin/tag-rules/1", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin delete rule: got %d, want 403", w.Code)
	}
}
