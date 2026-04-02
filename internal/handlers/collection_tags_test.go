package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createCollectionAndGetID is a helper that creates a collection and returns its integer ID.
func createCollectionAndGetID(t *testing.T, router http.Handler, token, name string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create collection %q: got %d, body: %s", name, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return int(resp["id"].(float64))
}

func TestCollectionTagsCRUD(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	collID := createCollectionAndGetID(t, router, token, "Tagged Collection")

	// Set tags on the collection
	tagBody, _ := json.Marshal(map[string]any{
		"tags": map[string][]string{
			"department": {"engineering"},
			"priority":   {"high"},
			"env":        {"prod"},
		},
	})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%d/tags", collID), bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("set collection tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var putResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &putResp)
	putTags := putResp["tags"].(map[string]any)
	if putTags["department"].([]any)[0] != "engineering" {
		t.Errorf("PUT response department = %v, want engineering", putTags["department"])
	}

	// Read tags back via GET
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d/tags", collID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get collection tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var getResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &getResp)
	tags := getResp["tags"].(map[string]any)

	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags["department"].([]any)[0] != "engineering" {
		t.Errorf("department = %v, want engineering", tags["department"])
	}
	if tags["priority"].([]any)[0] != "high" {
		t.Errorf("priority = %v, want high", tags["priority"])
	}
	if tags["env"].([]any)[0] != "prod" {
		t.Errorf("env = %v, want prod", tags["env"])
	}
}

func TestCollectionTagsReplace(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	collID := createCollectionAndGetID(t, router, token, "Replace Tags Collection")

	// Set initial tags
	tagBody, _ := json.Marshal(map[string]any{
		"tags": map[string][]string{"color": {"red"}, "size": {"large"}},
	})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%d/tags", collID), bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("set initial tags: got %d, body: %s", w.Code, w.Body.String())
	}

	// Replace with completely different tags
	tagBody, _ = json.Marshal(map[string]any{
		"tags": map[string][]string{"shape": {"circle"}},
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%d/tags", collID), bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("replace tags: got %d, body: %s", w.Code, w.Body.String())
	}

	// Read tags back — should only have the new set
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d/tags", collID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get tags after replace: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	tags := resp["tags"].(map[string]any)

	if len(tags) != 1 {
		t.Errorf("expected 1 tag after replace, got %d: %v", len(tags), tags)
	}
	if tags["shape"].([]any)[0] != "circle" {
		t.Errorf("shape = %v, want circle", tags["shape"])
	}
	if _, exists := tags["color"]; exists {
		t.Errorf("old tag 'color' should not exist after replace")
	}
	if _, exists := tags["size"]; exists {
		t.Errorf("old tag 'size' should not exist after replace")
	}
}

func TestCollectionTagsOwnership(t *testing.T) {
	router := setupTestRouter(t)
	userAToken := registerAndGetToken(t, router, "usera@test.com", "password123", "User", "A")
	userBToken := registerAndGetToken(t, router, "userb@test.com", "password123", "User", "B")

	// User A creates a collection and sets tags
	collID := createCollectionAndGetID(t, router, userAToken, "A's Private Collection")

	tagBody, _ := json.Marshal(map[string]any{
		"tags": map[string][]string{"owner": {"a"}},
	})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%d/tags", collID), bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userAToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("user A set tags: got %d, body: %s", w.Code, w.Body.String())
	}

	// User B tries to GET tags on User A's collection — should get 403
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d/tags", collID), nil)
	req.Header.Set("Authorization", "Bearer "+userBToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("user B GET tags: got %d, want 403", w.Code)
	}

	// User B tries to PUT tags on User A's collection — should get 403
	tagBody, _ = json.Marshal(map[string]any{
		"tags": map[string][]string{"hack": {"true"}},
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%d/tags", collID), bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userBToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("user B PUT tags: got %d, want 403", w.Code)
	}

	// Verify User A's tags are untouched
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%d/tags", collID), nil)
	req.Header.Set("Authorization", "Bearer "+userAToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	tags := resp["tags"].(map[string]any)
	if tags["owner"].([]any)[0] != "a" {
		t.Errorf("user A's tags were corrupted: got %v", tags)
	}
}

func TestCollectionTagsInheritOnAddFile(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, token)

	// Create a collection and set tags on it
	collID := createCollectionAndGetID(t, router, token, "Inherited Tags Collection")

	tagBody, _ := json.Marshal(map[string]any{
		"tags": map[string][]string{
			"department": {"legal"},
			"year":       {"2026"},
		},
	})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%d/tags", collID), bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("set collection tags: got %d, body: %s", w.Code, w.Body.String())
	}

	// Upload a file
	uploadUUID := uploadAndGetUUID(t, router, token, "contract.pdf")

	// Add the upload to the collection
	addBody, _ := json.Marshal(map[string]string{"upload_uuid": uploadUUID})
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v2/collections/%d/files", collID), bytes.NewReader(addBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("add file to collection: got %d, body: %s", w.Code, w.Body.String())
	}

	// Read the upload's tags — should have inherited the collection tags
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uploadUUID+"/tags", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get upload tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	tags := resp["tags"].(map[string]any)

	if tags["department"].([]any)[0] != "legal" {
		t.Errorf("inherited department = %v, want legal", tags["department"])
	}
	if tags["year"].([]any)[0] != "2026" {
		t.Errorf("inherited year = %v, want 2026", tags["year"])
	}
}
