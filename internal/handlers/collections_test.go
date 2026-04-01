package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAndListCollections(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create a collection
	body, _ := json.Marshal(map[string]string{
		"name":        "Q1 Tax Docs",
		"description": "Tax documents for Q1 2026",
	})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create collection: got %d, body: %s", w.Code, w.Body.String())
	}

	var coll map[string]any
	json.Unmarshal(w.Body.Bytes(), &coll)
	if coll["name"] != "Q1 Tax Docs" {
		t.Errorf("name = %v", coll["name"])
	}

	// List collections
	req = httptest.NewRequest("GET", "/api/v2/collections", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, body: %s", w.Code, w.Body.String())
	}

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	collections := listResp["collections"].([]any)
	if len(collections) != 1 {
		t.Errorf("expected 1 collection, got %d", len(collections))
	}
}

func TestCollectionHierarchy(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create parent
	body, _ := json.Marshal(map[string]string{"name": "Legal"})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var parent map[string]any
	json.Unmarshal(w.Body.Bytes(), &parent)
	parentID := parent["id"].(float64)

	// Create child
	body, _ = json.Marshal(map[string]any{"name": "Case #123", "parent_id": int(parentID)})
	req = httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create child: got %d, body: %s", w.Code, w.Body.String())
	}

	var child map[string]any
	json.Unmarshal(w.Body.Bytes(), &child)
	if child["parent_id"].(float64) != parentID {
		t.Errorf("parent_id = %v, want %v", child["parent_id"], parentID)
	}

	// List children of parent
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections?parent_id=%d", int(parentID)), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	children := listResp["collections"].([]any)
	if len(children) != 1 {
		t.Errorf("expected 1 child, got %d", len(children))
	}
}

func TestAddAndRemoveFileFromCollection(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Create collection
	body, _ := json.Marshal(map[string]string{"name": "My Files"})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var coll map[string]any
	json.Unmarshal(w.Body.Bytes(), &coll)
	collID := int(coll["id"].(float64))

	// Upload a file
	uploadUUID := uploadAndGetUUID(t, router, adminToken, "in-collection.txt")

	// Add file to collection
	addBody, _ := json.Marshal(map[string]string{"upload_uuid": uploadUUID})
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v2/collections/%d/files", collID), bytes.NewReader(addBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("add file: got %d, body: %s", w.Code, w.Body.String())
	}

	// Get collection — should show 1 file
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d", collID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var getResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &getResp)
	files := getResp["files"].([]any)
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}

	// Remove file from collection
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/collections/%d/files/%s", collID, uploadUUID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("remove file: got %d, body: %s", w.Code, w.Body.String())
	}

	// Get collection — should show 0 files
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d", collID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	json.Unmarshal(w.Body.Bytes(), &getResp)
	totalFiles := getResp["total_files"].(float64)
	if totalFiles != 0 {
		t.Errorf("expected 0 files after removal, got %v", totalFiles)
	}
}

func TestDeleteCollectionCascade(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Create parent with child
	body, _ := json.Marshal(map[string]string{"name": "Parent"})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var parent map[string]any
	json.Unmarshal(w.Body.Bytes(), &parent)
	parentID := int(parent["id"].(float64))

	body, _ = json.Marshal(map[string]any{"name": "Child", "parent_id": parentID})
	req = httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Delete parent — should cascade to child
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v2/collections/%d", parentID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete: got %d, body: %s", w.Code, w.Body.String())
	}

	// List should be empty
	req = httptest.NewRequest("GET", "/api/v2/collections", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	collections := listResp["collections"].([]any)
	if len(collections) != 0 {
		t.Errorf("expected 0 collections after cascade delete, got %d", len(collections))
	}
}

func TestOtherUserCantSeeCollections(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	// Admin creates collection
	body, _ := json.Marshal(map[string]string{"name": "Admin Only"})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var coll map[string]any
	json.Unmarshal(w.Body.Bytes(), &coll)
	collID := int(coll["id"].(float64))

	// User tries to get admin's collection
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d", collID), nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("other user access: got %d, want 404", w.Code)
	}
}
