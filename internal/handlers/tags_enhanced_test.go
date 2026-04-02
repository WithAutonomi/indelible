package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createMultipartUploadWithTags builds a multipart form body that includes a file,
// visibility, and a JSON-encoded tags field.
func createMultipartUploadWithTags(t *testing.T, filename string, data []byte, visibility string, tags map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if visibility != "" {
		writer.WriteField("visibility", visibility)
	}
	if tags != nil {
		tagsJSON, _ := json.Marshal(tags)
		writer.WriteField("tags", string(tagsJSON))
	}
	writer.Close()
	return body, writer.FormDataContentType()
}

func TestUploadWithTags(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	tags := map[string]string{
		"department": "engineering",
		"project":    "backend",
	}
	fileData := []byte("tagged upload content")
	body, contentType := createMultipartUploadWithTags(t, "tagged-file.txt", fileData, "public", tags)

	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("upload with tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	uploadUUID := resp["upload"].(map[string]any)["uuid"].(string)

	// Verify tags were set
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uploadUUID+"/tags", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var tagResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &tagResp)
	gotTags := tagResp["tags"].(map[string]any)
	if gotTags["department"].([]any)[0] != "engineering" {
		t.Errorf("department = %v, want engineering", gotTags["department"])
	}
	if gotTags["project"].([]any)[0] != "backend" {
		t.Errorf("project = %v, want backend", gotTags["project"])
	}
}

func TestTagSearch_Selector(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload two files and tag them differently
	uuid1 := uploadAndGetUUID(t, router, adminToken, "eng-doc.txt")
	uuid2 := uploadAndGetUUID(t, router, adminToken, "sales-doc.txt")

	tagBody, _ := json.Marshal(map[string]any{"tags": map[string][]string{"department": {"engineering"}}})
	req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid1+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tag file 1: got %d, body: %s", w.Code, w.Body.String())
	}

	tagBody, _ = json.Marshal(map[string]any{"tags": map[string][]string{"department": {"sales"}}})
	req = httptest.NewRequest("PUT", "/api/v2/uploads/"+uuid2+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tag file 2: got %d, body: %s", w.Code, w.Body.String())
	}

	// Search with selector=department=engineering
	req = httptest.NewRequest("GET", "/api/v2/tags/search?selector=department=engineering", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("selector search: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	results := resp["results"].([]any)
	if len(results) != 1 {
		t.Errorf("expected 1 result for selector=department=engineering, got %d", len(results))
	}
	if len(results) > 0 {
		firstResult := results[0].(map[string]any)
		upload := firstResult["upload"].(map[string]any)
		if upload["original_filename"] != "eng-doc.txt" {
			t.Errorf("filename = %v, want eng-doc.txt", upload["original_filename"])
		}
	}
}

func TestBulkTagByUUIDs(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload three files
	uuid1 := uploadAndGetUUID(t, router, adminToken, "bulk-file1.txt")
	uuid2 := uploadAndGetUUID(t, router, adminToken, "bulk-file2.txt")
	uuid3 := uploadAndGetUUID(t, router, adminToken, "bulk-file3.txt")

	// Bulk add tags to the first two
	bulkBody, _ := json.Marshal(map[string]any{
		"upload_uuids": []string{uuid1, uuid2},
		"add_tags": map[string][]string{
			"team":   {"platform"},
			"status": {"reviewed"},
		},
	})
	req := httptest.NewRequest("POST", "/api/v2/tags/bulk", bytes.NewReader(bulkBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("bulk tag: got %d, body: %s", w.Code, w.Body.String())
	}

	var bulkResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &bulkResp)
	affected := bulkResp["affected"].(float64)
	if affected != 2 {
		t.Errorf("affected = %v, want 2", affected)
	}

	// Verify file 1 has the tags
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uuid1+"/tags", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var tagResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &tagResp)
	tags1 := tagResp["tags"].(map[string]any)
	if tags1["team"].([]any)[0] != "platform" {
		t.Errorf("file1 team = %v, want platform", tags1["team"])
	}
	if tags1["status"].([]any)[0] != "reviewed" {
		t.Errorf("file1 status = %v, want reviewed", tags1["status"])
	}

	// Verify file 2 has the tags
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uuid2+"/tags", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	json.Unmarshal(w.Body.Bytes(), &tagResp)
	tags2 := tagResp["tags"].(map[string]any)
	if tags2["team"].([]any)[0] != "platform" {
		t.Errorf("file2 team = %v, want platform", tags2["team"])
	}

	// Verify file 3 does NOT have the tags
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uuid3+"/tags", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	json.Unmarshal(w.Body.Bytes(), &tagResp)
	tags3 := tagResp["tags"].(map[string]any)
	if len(tags3) != 0 {
		t.Errorf("file3 should have 0 tags, got %d: %v", len(tags3), tags3)
	}
}

func TestTagFacets(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload files and tag them
	uuids := make([]string, 3)
	for i := 0; i < 3; i++ {
		uuids[i] = uploadAndGetUUID(t, router, adminToken, fmt.Sprintf("facet-file%d.txt", i))
	}

	// Tag file 0 and 1 with department=engineering, file 2 with department=sales
	for i := 0; i < 2; i++ {
		tagBody, _ := json.Marshal(map[string]any{"tags": map[string][]string{"department": {"engineering"}}})
		req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuids[i]+"/tags", bytes.NewReader(tagBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tag file %d: got %d, body: %s", i, w.Code, w.Body.String())
		}
	}

	tagBody, _ := json.Marshal(map[string]any{"tags": map[string][]string{"department": {"sales"}}})
	req := httptest.NewRequest("PUT", "/api/v2/uploads/"+uuids[2]+"/tags", bytes.NewReader(tagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tag file 2: got %d, body: %s", w.Code, w.Body.String())
	}

	// Get facets
	req = httptest.NewRequest("GET", "/api/v2/tags/facets", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("facets: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	facets := resp["facets"].([]any)

	// We should have 2 facet entries: department=engineering (count 2) and department=sales (count 1)
	if len(facets) != 2 {
		t.Fatalf("expected 2 facet entries, got %d: %v", len(facets), facets)
	}

	// Build a lookup for verification
	facetMap := make(map[string]float64)
	for _, f := range facets {
		entry := f.(map[string]any)
		key := fmt.Sprintf("%s=%s", entry["key"], entry["value"])
		facetMap[key] = entry["count"].(float64)
	}

	if facetMap["department=engineering"] != 2 {
		t.Errorf("department=engineering count = %v, want 2", facetMap["department=engineering"])
	}
	if facetMap["department=sales"] != 1 {
		t.Errorf("department=sales count = %v, want 1", facetMap["department=sales"])
	}
}

func TestCollectionTagInheritance(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Create a collection
	collBody, _ := json.Marshal(map[string]string{
		"name":        "Engineering Docs",
		"description": "All engineering documents",
	})
	req := httptest.NewRequest("POST", "/api/v2/collections", bytes.NewReader(collBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create collection: got %d, body: %s", w.Code, w.Body.String())
	}

	var collResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &collResp)
	collID := collResp["id"].(float64)

	// Set tags on the collection
	collTagBody, _ := json.Marshal(map[string]any{
		"tags": map[string][]string{
			"team":       {"engineering"},
			"compliance": {"internal"},
		},
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/collections/%.0f/tags", collID), bytes.NewReader(collTagBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("set collection tags: got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify collection tags were stored
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v2/collections/%.0f/tags", collID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get collection tags: got %d, body: %s", w.Code, w.Body.String())
	}

	var collTagResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &collTagResp)
	collTags := collTagResp["tags"].(map[string]any)
	if collTags["team"].([]any)[0] != "engineering" {
		t.Errorf("collection team = %v, want engineering", collTags["team"])
	}
	if collTags["compliance"].([]any)[0] != "internal" {
		t.Errorf("collection compliance = %v, want internal", collTags["compliance"])
	}

	// Upload a file (no tags on the file yet)
	uploadUUID := uploadAndGetUUID(t, router, adminToken, "inherit-test.txt")

	// Add the file to the collection -- this should trigger tag inheritance
	addBody, _ := json.Marshal(map[string]string{"upload_uuid": uploadUUID})
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v2/collections/%.0f/files", collID), bytes.NewReader(addBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("add file to collection: got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify the file inherited the collection tags
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uploadUUID+"/tags", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get file tags after inheritance: got %d, body: %s", w.Code, w.Body.String())
	}

	var fileTagResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &fileTagResp)
	fileTags := fileTagResp["tags"].(map[string]any)
	if fileTags["team"].([]any)[0] != "engineering" {
		t.Errorf("inherited team = %v, want engineering", fileTags["team"])
	}
	if fileTags["compliance"].([]any)[0] != "internal" {
		t.Errorf("inherited compliance = %v, want internal", fileTags["compliance"])
	}
}
