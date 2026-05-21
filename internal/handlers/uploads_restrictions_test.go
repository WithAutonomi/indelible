package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// V2-327: per-user + per-token upload restrictions are enforced at the upload
// handler with the chain token override > user override > system default.

// createMultipartUploadWithType builds the same form as createMultipartUpload
// but lets the test pin an explicit Content-Type for the file part, which the
// upload handler reads from the part header (not the form field).
func createMultipartUploadWithType(t *testing.T, filename, contentType string, data []byte, visibility string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create form part: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatalf("write form part: %v", err)
	}
	if visibility != "" {
		writer.WriteField("visibility", visibility)
	}
	writer.Close()
	return body, writer.FormDataContentType()
}

// adminUpdateUserRestrictions PATCHes a user via the admin API and returns the response.
func adminUpdateUserRestrictions(t *testing.T, router http.Handler, adminToken string, userID int64, maxSize *int64, allowedTypes []string) map[string]any {
	t.Helper()
	payload := map[string]any{}
	if maxSize != nil {
		payload["max_file_size_bytes"] = *maxSize
	}
	if allowedTypes != nil {
		payload["allowed_file_types"] = allowedTypes
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v2/admin/users/%d", userID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin update user: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}

// createTokenWith creates a token for the authenticated user via /api/v2/tokens
// and returns the secret + the parsed token object.
func createTokenWith(t *testing.T, router http.Handler, ownerToken string, payload map[string]any) (string, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v2/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create token: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["secret"].(string), resp["token"].(map[string]any)
}

func TestUploadRestrictions_AdminUpdateUserExposesFields(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	maxSize := int64(1 << 20) // 1 MB
	resp := adminUpdateUserRestrictions(t, router, adminToken, 1, &maxSize, []string{"image/*", "application/pdf"})

	got, ok := resp["max_file_size_bytes"].(float64)
	if !ok || int64(got) != 1<<20 {
		t.Errorf("max_file_size_bytes = %v, want %d", resp["max_file_size_bytes"], 1<<20)
	}
	types, ok := resp["allowed_file_types"].([]any)
	if !ok || len(types) != 2 {
		t.Fatalf("allowed_file_types = %v, want 2 entries", resp["allowed_file_types"])
	}
}

func TestUploadRestrictions_TokenCreateExposesFields(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	maxSize := int64(500 << 10) // 500 KB
	_, tok := createTokenWith(t, router, adminToken, map[string]any{
		"name":                "Restricted Token",
		"permissions":         []string{"read", "write"},
		"max_file_size_bytes": maxSize,
		"allowed_file_types":  []string{"application/pdf"},
	})

	got, ok := tok["max_file_size_bytes"].(float64)
	if !ok || int64(got) != 500<<10 {
		t.Errorf("max_file_size_bytes = %v, want %d", tok["max_file_size_bytes"], 500<<10)
	}
	types, ok := tok["allowed_file_types"].([]any)
	if !ok || len(types) != 1 || types[0] != "application/pdf" {
		t.Errorf("allowed_file_types = %v, want [application/pdf]", tok["allowed_file_types"])
	}
}

func TestUploadRestrictions_PerUserMaxSize_413(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Cap admin (user id=1) at 100 bytes.
	maxSize := int64(100)
	adminUpdateUserRestrictions(t, router, adminToken, 1, &maxSize, nil)

	// 200 bytes > 100-byte limit → 413.
	body, ct := createMultipartUpload(t, "doc.txt", bytes.Repeat([]byte("a"), 200), "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: got %d, want 413, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "file_too_large") {
		t.Errorf("response missing file_too_large code: %s", w.Body.String())
	}
}

func TestUploadRestrictions_PerUserAllowedTypes_415(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Restrict admin to PDFs only.
	adminUpdateUserRestrictions(t, router, adminToken, 1, nil, []string{"application/pdf"})

	// Upload a text/plain file → 415.
	body, ct := createMultipartUploadWithType(t, "doc.txt", "text/plain", []byte("hello"), "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("disallowed type: got %d, want 415, body: %s", w.Code, w.Body.String())
	}
}

func TestUploadRestrictions_PerTokenMaxSizeOverridesUser(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// User cap = 10 KB, token cap = 100 bytes. Token is tighter; token wins.
	userMax := int64(10 << 10)
	adminUpdateUserRestrictions(t, router, adminToken, 1, &userMax, nil)
	tokenMax := int64(100)
	apiSecret, _ := createTokenWith(t, router, adminToken, map[string]any{
		"name":                "Tight Token",
		"permissions":         []string{"read", "write"},
		"max_file_size_bytes": tokenMax,
	})

	// 500 bytes — under user limit, over token limit → 413.
	body, ct := createMultipartUpload(t, "doc.txt", bytes.Repeat([]byte("a"), 500), "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+apiSecret)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("token-tighter cap: got %d, want 413, body: %s", w.Code, w.Body.String())
	}
}

func TestUploadRestrictions_PerTokenAllowedTypesOverridesUser(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// User allows text/*. Token narrows to application/pdf only.
	adminUpdateUserRestrictions(t, router, adminToken, 1, nil, []string{"text/*"})
	apiSecret, _ := createTokenWith(t, router, adminToken, map[string]any{
		"name":               "PDF Only Token",
		"permissions":        []string{"read", "write"},
		"allowed_file_types": []string{"application/pdf"},
	})

	// Upload text/plain — passes user list, rejected by token list → 415.
	body, ct := createMultipartUploadWithType(t, "doc.txt", "text/plain", []byte("hello"), "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+apiSecret)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("token-narrowed types: got %d, want 415, body: %s", w.Code, w.Body.String())
	}
}

func TestUploadRestrictions_WithinLimitsSucceeds(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	maxSize := int64(10 << 10) // 10 KB
	adminUpdateUserRestrictions(t, router, adminToken, 1, &maxSize, []string{"text/*"})

	body, ct := createMultipartUploadWithType(t, "small.txt", "text/plain", []byte("under the limit"), "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("within-limits upload: got %d, want 202, body: %s", w.Code, w.Body.String())
	}
}
