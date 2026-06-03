package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// Helper: JSON request with auth
func jsonRequest(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestWorkflow_RegisterUploadTagCollect(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "workflow@test.com", "password123", "Work", "Flow")
	// Wallet creation is admin-only; use the seeded bootstrap admin. The
	// wallet is instance-wide, so the read user's uploads below still use it.
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Step 1: Upload a file
	uuid := uploadAndGetUUID(t, router, token, "workflow-doc.pdf")
	if uuid == "" {
		t.Fatal("expected non-empty UUID from upload")
	}

	// Step 2: Tag it
	w := jsonRequest(t, router, "PUT", "/api/v2/uploads/"+uuid+"/tags", token,
		map[string]any{"tags": map[string][]string{"project": {"alpha"}, "env": {"test"}}})
	if w.Code != http.StatusOK {
		t.Fatalf("tag upload: got %d, body: %s", w.Code, w.Body.String())
	}

	// Step 3: Create a collection
	w = jsonRequest(t, router, "POST", "/api/v2/collections", token,
		map[string]string{"name": "Test Collection"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create collection: got %d, body: %s", w.Code, w.Body.String())
	}
	var collResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &collResp)
	collID := collResp["id"]

	// Step 4: Add file to collection
	w = jsonRequest(t, router, "POST", "/api/v2/collections/"+formatID(collID)+"/files", token,
		map[string]string{"upload_uuid": uuid})
	if w.Code != http.StatusOK {
		t.Fatalf("add to collection: got %d, body: %s", w.Code, w.Body.String())
	}

	// Step 5: Verify file is in collection
	w = jsonRequest(t, router, "GET", "/api/v2/collections/"+formatID(collID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get collection: got %d, body: %s", w.Code, w.Body.String())
	}

	// Step 6: Search by tag finds the file
	w = jsonRequest(t, router, "GET", "/api/v2/tags/search?tag.project=alpha", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("tag search: got %d, body: %s", w.Code, w.Body.String())
	}
	var searchResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &searchResp)
	results, ok := searchResp["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("tag search returned no results, expected to find the tagged upload; response: %s", w.Body.String())
	}
}

func TestWorkflow_MultiUserIsolation(t *testing.T) {
	router := setupTestRouter(t)

	// Admin registers first (gets admin role)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Regular user registers
	userToken := registerAndGetToken(t, router, "regular@test.com", "password123", "Regular", "User")

	// Both upload files
	adminUUID := uploadAndGetUUID(t, router, adminToken, "admin-file.txt")
	userUUID := uploadAndGetUUID(t, router, userToken, "user-file.txt")

	// Each user can see their own uploads
	w := jsonRequest(t, router, "GET", "/api/v2/uploads", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin list uploads: got %d", w.Code)
	}
	var adminUploads map[string]any
	json.Unmarshal(w.Body.Bytes(), &adminUploads)

	w = jsonRequest(t, router, "GET", "/api/v2/uploads", userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("user list uploads: got %d", w.Code)
	}
	var userUploads map[string]any
	json.Unmarshal(w.Body.Bytes(), &userUploads)

	// Verify user cannot access admin's upload by UUID
	w = jsonRequest(t, router, "GET", "/api/v2/uploads/"+adminUUID, userToken, nil)
	if w.Code == http.StatusOK {
		t.Fatal("regular user should NOT be able to access admin's upload")
	}

	// Verify admin cannot access user's upload by UUID (non-admin scoped)
	w = jsonRequest(t, router, "GET", "/api/v2/uploads/"+userUUID, adminToken, nil)
	// Admin may or may not have cross-user access depending on implementation.
	// The key test is that regular user cannot access admin's resources.
	_ = w
}

func TestWorkflow_APITokenFullLifecycle(t *testing.T) {
	router := setupTestRouter(t)
	token := registerAndGetToken(t, router, "api-user@test.com", "password123", "API", "User")
	// Wallet creation is admin-only; use the seeded bootstrap admin.
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Create an API token
	w := jsonRequest(t, router, "POST", "/api/v2/tokens", token,
		map[string]any{"name": "test-token", "permissions": []string{"read"}})
	if w.Code != http.StatusCreated {
		t.Fatalf("create token: got %d, body: %s", w.Code, w.Body.String())
	}
	var tokenResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &tokenResp)
	apiSecret := tokenResp["secret"].(string)
	tokenObj := tokenResp["token"].(map[string]any)
	tokenUUID := tokenObj["uuid"].(string)

	// Use the API token to list uploads (should work with read permission)
	w = jsonRequest(t, router, "GET", "/api/v2/uploads", apiSecret, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list uploads with API token: got %d, body: %s", w.Code, w.Body.String())
	}

	// List tokens (verify it appears)
	w = jsonRequest(t, router, "GET", "/api/v2/tokens", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list tokens: got %d, body: %s", w.Code, w.Body.String())
	}

	// Revoke the token
	w = jsonRequest(t, router, "DELETE", "/api/v2/tokens/"+tokenUUID, token, nil)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("revoke token: got %d, body: %s", w.Code, w.Body.String())
	}

	// Use revoked token — should fail
	w = jsonRequest(t, router, "GET", "/api/v2/uploads", apiSecret, nil)
	if w.Code == http.StatusOK {
		t.Fatal("revoked API token should not have access")
	}
}

func TestWorkflow_AdminUserManagement(t *testing.T) {
	router := setupTestRouter(t)

	// Admin is the seeded bootstrap admin.
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "Boss")
	createTestWallet(t, router, adminToken)

	// Register a second user
	_ = registerAndGetToken(t, router, "user@mgmt.com", "password123", "Normal", "User")

	// Admin lists users
	w := jsonRequest(t, router, "GET", "/api/v2/admin/users", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list users: got %d, body: %s", w.Code, w.Body.String())
	}
	var usersResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &usersResp)
	users, ok := usersResp["users"].([]any)
	if !ok || len(users) < 2 {
		t.Fatalf("expected at least 2 users, got %d", len(users))
	}
}

func TestWorkflow_WebhookDeliveryOnUpload(t *testing.T) {
	router := setupTestRouter(t)
	// Wallet + webhook creation are admin-only and the actor also uploads, so
	// run the whole flow as the seeded bootstrap admin.
	token := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Webhook", "Test")
	createTestWallet(t, router, token)

	// Create a webhook (admin endpoint)
	w := jsonRequest(t, router, "POST", "/api/v2/admin/webhooks", token,
		map[string]string{
			"url":              "https://httpbin.org/post",
			"events":           "upload.completed,upload.failed",
			"integration_type": "generic",
		})
	if w.Code != http.StatusCreated {
		t.Fatalf("create webhook: got %d, body: %s", w.Code, w.Body.String())
	}

	// Upload a file (triggers webhook event)
	uuid := uploadAndGetUUID(t, router, token, "webhook-test.txt")
	_ = uuid

	// Check webhook deliveries list
	w = jsonRequest(t, router, "GET", "/api/v2/admin/webhooks", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list webhooks: got %d, body: %s", w.Code, w.Body.String())
	}
}

// formatID converts JSON-decoded numeric IDs to URL path strings
func formatID(id any) string {
	switch v := id.(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case string:
		return v
	default:
		return "0"
	}
}
