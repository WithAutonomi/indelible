package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func uploadAndGetUUID(t *testing.T, router http.Handler, token string, filename string) string {
	t.Helper()
	fileData := []byte("file content for " + filename)
	body, contentType := createMultipartUpload(t, filename, fileData, "public")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("upload %s: got %d, body: %s", filename, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["upload"].(map[string]any)["uuid"].(string)
}

func TestUpdateAndGetTags(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	uuid := uploadAndGetUUID(t, router, adminToken, "tagged-file.txt")

	// Set tags
	tagBody, _ := json.Marshal(map[string]any{
		"tags": map[string]string{
			"department": "legal",
			"project":    "alpha",
			"client":     "acme",
		},
	})
	req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	tags := resp["tags"].(map[string]any)
	if tags["department"] != "legal" {
		t.Errorf("department = %v, want legal", tags["department"])
	}
	if tags["project"] != "alpha" {
		t.Errorf("project = %v, want alpha", tags["project"])
	}
}

func TestUpdateTags_Replace(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	uuid := uploadAndGetUUID(t, router, adminToken, "replace-tags.txt")

	// Set initial tags
	tagBody, _ := json.Marshal(map[string]any{"tags": map[string]string{"a": "1", "b": "2"}})
	req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Replace with different tags
	tagBody, _ = json.Marshal(map[string]any{"tags": map[string]string{"c": "3"}})
	req = httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("replace tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	tags := resp["tags"].(map[string]any)

	// Should only have "c", not "a" or "b"
	if len(tags) != 1 {
		t.Errorf("expected 1 tag after replace, got %d: %v", len(tags), tags)
	}
	if tags["c"] != "3" {
		t.Errorf("c = %v, want 3", tags["c"])
	}
}

func TestSearchByTags(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Upload and tag two files
	uuid1 := uploadAndGetUUID(t, router, adminToken, "legal-doc.pdf")
	uuid2 := uploadAndGetUUID(t, router, adminToken, "finance-report.xlsx")

	// Tag file 1
	tagBody, _ := json.Marshal(map[string]any{"tags": map[string]string{"department": "legal"}})
	req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid1+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Tag file 2
	tagBody, _ = json.Marshal(map[string]any{"tags": map[string]string{"department": "finance"}})
	req = httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid2+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Search by tag
	req = httptest.NewRequest("GET", "/api/v2/tags/search?tag.department=legal", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("search: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	results := resp["results"].([]any)

	if len(results) != 1 {
		t.Errorf("expected 1 result for department=legal, got %d", len(results))
	}

	// Search by filename
	req = httptest.NewRequest("GET", "/api/v2/tags/search?q=finance", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	json.Unmarshal(w.Body.Bytes(), &resp)
	results = resp["results"].([]any)
	if len(results) != 1 {
		t.Errorf("expected 1 result for q=finance, got %d", len(results))
	}
}

func TestUpdateTags_OtherUserCantTag(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	uuid := uploadAndGetUUID(t, router, adminToken, "admin-file.txt")

	tagBody, _ := json.Marshal(map[string]any{"tags": map[string]string{"hack": "true"}})
	req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("other user tagging: got %d, want 404", w.Code)
	}
}
