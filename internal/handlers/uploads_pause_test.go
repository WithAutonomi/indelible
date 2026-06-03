package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// TestCreateUpload_PausedByDiskPressure verifies V2-421: when the disk-alert
// worker has set the "uploads_paused" flag, CreateUpload sheds the request with
// 503 + code "uploads_paused" before buffering any temp file (the check sits
// ahead of ParseMultipartForm).
func TestCreateUpload_PausedByDiskPressure(t *testing.T) {
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          seedAdminEmail,
		AdminPassword:       seedAdminPassword,
	}
	db := dbtest.OpenDB(t)
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	router := handlers.NewRouter(cfg, db, nil)

	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	// Simulate the disk-alert worker flagging critical disk usage.
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('uploads_paused', 'true')`); err != nil {
		t.Fatalf("set uploads_paused: %v", err)
	}

	body, contentType := createMultipartUpload(t, "doc.txt", []byte("payload"), "private")
	req := httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("paused upload: got %d, want 503; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != "uploads_paused" {
		t.Errorf("code = %v, want uploads_paused", resp["code"])
	}

	// Clearing the flag restores normal acceptance.
	if _, err := db.Exec(`UPDATE settings SET value = 'false' WHERE key = 'uploads_paused'`); err != nil {
		t.Fatalf("clear uploads_paused: %v", err)
	}
	body, contentType = createMultipartUpload(t, "doc2.txt", []byte("payload"), "private")
	req = httptest.NewRequest("POST", "/api/v2/uploads", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("unpaused upload: got %d, want 202; body: %s", w.Code, w.Body.String())
	}
}
