package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// File-access audit logging: upload create, delete, and cross-user denied
// attempts must each write an audit row (surfaced in the admin Logs UI via the
// same audit_log table countAuditEvents queries). Download *success* isn't
// covered here — it requires antd to fetch the bytes before the audit fires —
// but the security-relevant download-denied path is.
func TestFileAccessAudit_UploadDeleteAndDenied(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	// A second, non-owner user (self-registered → read; upload routes have no
	// write gate, so this reaches the handler's ownership check).
	otherToken := registerAndGetToken(t, router, "mallory@test.com", "password123", "Mallory", "Other")

	// Upload as admin → file_uploaded.
	uuid := uploadAndGetUUID(t, router, adminToken, "audit-doc.txt")
	if n := countAuditEvents(t, router, adminToken, "file_uploaded"); n != 1 {
		t.Errorf("file_uploaded audit count = %d, want 1", n)
	}

	// Non-owner download attempt → 404 + file_download_denied.
	req := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner download: got %d, want 404", w.Code)
	}
	if n := countAuditEvents(t, router, adminToken, "file_download_denied"); n != 1 {
		t.Errorf("file_download_denied audit count = %d, want 1", n)
	}

	// Non-owner delete attempt → 404 + file_delete_denied.
	req = httptest.NewRequest("DELETE", "/api/v2/uploads/"+uuid, nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner delete: got %d, want 404", w.Code)
	}
	if n := countAuditEvents(t, router, adminToken, "file_delete_denied"); n != 1 {
		t.Errorf("file_delete_denied audit count = %d, want 1", n)
	}

	// Owner delete → 200 + file_deleted. Queued uploads are intentionally
	// non-deletable ("only failed or completed uploads can be deleted"); mark it
	// failed so the delete proceeds (no antd in tests to complete it normally).
	if _, err := db.Exec(`UPDATE uploads SET status = 'failed' WHERE uuid = ?`, uuid); err != nil {
		t.Fatalf("set upload failed: %v", err)
	}
	req = httptest.NewRequest("DELETE", "/api/v2/uploads/"+uuid, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner delete: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if n := countAuditEvents(t, router, adminToken, "file_deleted"); n != 1 {
		t.Errorf("file_deleted audit count = %d, want 1", n)
	}
}
