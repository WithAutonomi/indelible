package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getAuditVerify(t *testing.T, router http.Handler, token string) (intact bool, count, brokenAt int64) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit/verify", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: got %d, body: %s", w.Code, w.Body.String())
	}
	var r struct {
		Intact   bool  `json:"intact"`
		Count    int64 `json:"count"`
		BrokenAt int64 `json:"broken_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	return r.Intact, r.Count, r.BrokenAt
}

// V2-452: the audit log is hash-chained, so any post-hoc edit/deletion of a row
// is detectable. Verify reports intact on a clean chain and pinpoints the first
// broken row after tampering.
func TestAuditChain_VerifyAndTamperDetection(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")

	// Generate a deterministic extra audit row (user_created) on top of the
	// login row(s) that acquiring the admin token already produced.
	body, _ := json.Marshal(map[string]string{
		"email": "chain@test.com", "password": "password123",
		"first_name": "Chain", "last_name": "User", "permissions": "read",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: got %d, body: %s", w.Code, w.Body.String())
	}

	// Clean chain → intact.
	intact, count, broken := getAuditVerify(t, router, adminToken)
	if !intact {
		t.Fatalf("expected intact chain, got broken_at=%d", broken)
	}
	if count < 2 {
		t.Errorf("chained row count = %d, want >= 2", count)
	}

	// Tamper the earliest chained row directly in the DB.
	var firstID int64
	if err := db.QueryRow(`SELECT id FROM audit_log WHERE row_hash != '' ORDER BY id ASC LIMIT 1`).Scan(&firstID); err != nil {
		t.Fatalf("find first chained row: %v", err)
	}
	if _, err := db.Exec(`UPDATE audit_log SET detail = 'TAMPERED' WHERE id = ?`, firstID); err != nil {
		t.Fatalf("tamper row: %v", err)
	}

	// Chain now broken, pinpointed at the tampered row.
	intact2, _, broken2 := getAuditVerify(t, router, adminToken)
	if intact2 {
		t.Fatal("expected chain to be reported broken after tampering")
	}
	if broken2 != firstID {
		t.Errorf("broken_at = %d, want %d (the tampered row)", broken2, firstID)
	}
}
