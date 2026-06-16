package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// countFileAccessEvents queries the plain file_access_log via its admin
// endpoint (V2-514). Download-route events (file_downloaded,
// file_download_denied) live here, off the audit hash-chain, so they are
// counted separately from countAuditEvents (which reads audit_log).
func countFileAccessEvents(t *testing.T, router http.Handler, adminToken, eventType string) int {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v2/admin/logs/file-access?event_type="+eventType, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query file-access logs (%s): got %d, body: %s", eventType, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return int(resp["total"].(float64))
}

// File-access logging: file mutations (upload create, delete) and unauthorized-
// mutation attempts stay in the tamper-evident audit_log (countAuditEvents),
// while download-route reads/denials go to the plain file_access_log
// (countFileAccessEvents) so a reader fleet never touches the chain (V2-514).
// Download *success* isn't covered here — it requires antd to fetch the bytes
// before the event fires — but the security-relevant download-denied path is.
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
	// Lives in file_access_log, NOT the audit hash-chain (V2-514).
	if n := countFileAccessEvents(t, router, adminToken, "file_download_denied"); n != 1 {
		t.Errorf("file_download_denied file-access count = %d, want 1", n)
	}
	if n := countAuditEvents(t, router, adminToken, "file_download_denied"); n != 0 {
		t.Errorf("file_download_denied must not be chained in audit_log; count = %d, want 0", n)
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

// V2-514: file-access writes go to a separate plain table, so they must neither
// join nor break the audit hash-chain. A denied download (which writes
// file_download_denied) interleaved between two chained events leaves the chain
// verifiably intact — proving the download path is chain-free, which is what
// lets a reader fleet serve downloads without forking the chain. Uses
// getAuditVerify from audit_chain_test.go (same package).
func TestFileAccess_DoesNotTouchAuditChain(t *testing.T) {
	router, _ := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	otherToken := registerAndGetToken(t, router, "mallory@test.com", "password123", "Mallory", "Other")
	createTestWallet(t, router, adminToken)

	uuid := uploadAndGetUUID(t, router, adminToken, "chain-doc.txt")

	// Denied download → a file_access_log write between chained events.
	req := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner download: got %d, want 404", w.Code)
	}

	intact, count, broken := getAuditVerify(t, router, adminToken)
	if !intact {
		t.Fatalf("audit chain broken after file-access write, broken_at=%d", broken)
	}
	if count < 1 {
		t.Errorf("chained row count = %d, want >= 1 (login/upload events)", count)
	}
}
