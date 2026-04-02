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

func createMultipartUpload(t *testing.T, filename string, data []byte, visibility string) (*bytes.Buffer, string) {
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
	writer.Close()
	return body, writer.FormDataContentType()
}

func TestCreateUpload_Queued(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload a file
	fileData := []byte("hello world, this is a test file for upload")
	body, contentType := createMultipartUpload(t, "test-doc.txt", fileData, "public")

	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("create upload: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	upload := resp["upload"].(map[string]any)
	if upload["status"] != "queued" {
		t.Errorf("status = %v, want queued", upload["status"])
	}
	if upload["original_filename"] != "test-doc.txt" {
		t.Errorf("original_filename = %v, want test-doc.txt", upload["original_filename"])
	}
	if upload["visibility"] != "public" {
		t.Errorf("visibility = %v, want public", upload["visibility"])
	}
	if upload["file_size"].(float64) != float64(len(fileData)) {
		t.Errorf("file_size = %v, want %d", upload["file_size"], len(fileData))
	}
	if upload["uuid"] == nil || upload["uuid"] == "" {
		t.Error("missing uuid")
	}
}

func TestCreateUpload_DefaultPrivate(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload without visibility — should default to private
	fileData := []byte("private file content")
	body, contentType := createMultipartUpload(t, "secret.bin", fileData, "")

	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("create upload: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	upload := resp["upload"].(map[string]any)
	if upload["visibility"] != "private" {
		t.Errorf("visibility = %v, want private", upload["visibility"])
	}
}

func TestListUploads(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload two files
	for i := 0; i < 2; i++ {
		fileData := []byte(fmt.Sprintf("file content %d", i))
		body, contentType := createMultipartUpload(t, fmt.Sprintf("file%d.txt", i), fileData, "public")
		req := httptest.NewRequest("POST", "/api/v2/uploads", body)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("upload %d: got %d", i, w.Code)
		}
	}

	// List uploads
	req := httptest.NewRequest("GET", "/api/v2/uploads", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list uploads: got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	uploads := resp["uploads"].([]any)
	if len(uploads) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(uploads))
	}
	total := resp["total"].(float64)
	if total != 2 {
		t.Errorf("total = %v, want 2", total)
	}
}

func TestGetUpload(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload a file
	fileData := []byte("get me later")
	body, contentType := createMultipartUpload(t, "retrieve.txt", fileData, "public")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	uploadUUID := createResp["upload"].(map[string]any)["uuid"].(string)

	// Get the upload
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uploadUUID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get upload: got %d, body: %s", w.Code, w.Body.String())
	}

	var upload map[string]any
	json.Unmarshal(w.Body.Bytes(), &upload)
	if upload["uuid"] != uploadUUID {
		t.Errorf("uuid = %v, want %s", upload["uuid"], uploadUUID)
	}
	if upload["original_filename"] != "retrieve.txt" {
		t.Errorf("filename = %v, want retrieve.txt", upload["original_filename"])
	}
}

func TestGetUpload_OtherUserCantSee(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)
	userToken := registerAndGetToken(t, router, "user@test.com", "password123", "Normal", "User")

	// Admin uploads a file
	fileData := []byte("admin only file")
	body, contentType := createMultipartUpload(t, "admin-file.txt", fileData, "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	uploadUUID := createResp["upload"].(map[string]any)["uuid"].(string)

	// Non-admin user tries to get admin's upload
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uploadUUID, nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("other user accessing upload: got %d, want 404", w.Code)
	}
}

func TestDownload_NotCompleted(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Upload a file (will be in "queued" state, not "completed")
	fileData := []byte("cant download yet")
	body, contentType := createMultipartUpload(t, "pending.txt", fileData, "public")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	uploadUUID := createResp["upload"].(map[string]any)["uuid"].(string)

	// Try to download — should fail because not completed
	req = httptest.NewRequest("GET", "/api/v2/uploads/"+uploadUUID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("download not-ready: got %d, want 409", w.Code)
	}
}

func TestCreateUpload_NoFile(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// POST without a file
	req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("no file upload: got %d, want 400", w.Code)
	}
}

func TestCreateUpload_InvalidVisibility(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	fileData := []byte("some data")
	body, contentType := createMultipartUpload(t, "test.txt", fileData, "invalid")

	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid visibility: got %d, want 400", w.Code)
	}
}
