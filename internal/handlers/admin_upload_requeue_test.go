package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
	"github.com/WithAutonomi/indelible/internal/worker"
)

// setupRequeueWindowRouter mirrors setupTestRouter with the worker role
// chosen: the default test config is a reader (WorkersEnabled false).
func setupRequeueWindowRouter(t *testing.T, workersEnabled bool) http.Handler {
	t.Helper()
	t.Cleanup(worker.ResetStuckRequeueAfter)
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             closedAntdURL(t),
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          seedAdminEmail,
		AdminPassword:       seedAdminPassword,
		WorkersEnabled:      workersEnabled,
	}
	db := dbtest.OpenDB(t)
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('registration_enabled', 'true')`); err != nil {
		t.Fatalf("enable registration: %v", err)
	}
	return handlers.NewRouter(cfg, db, nil, nil)
}

func requeueWindowCall(t *testing.T, router http.Handler, method, token, body string) (int, handlers.UploadRequeueWindow) {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "/api/v2/admin/upload-requeue-window", reader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp handlers.UploadRequeueWindow
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v (%s)", err, w.Body.String())
		}
	}
	return w.Code, resp
}

// The stuck-upload requeue window can be read, overridden and reset by an
// admin; every change is audited; bad values are refused (V2-1773).
func TestAdminUploadRequeueWindow(t *testing.T) {
	router := setupRequeueWindowRouter(t, true)
	admin := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	code, got := requeueWindowCall(t, router, http.MethodGet, admin, "")
	if code != http.StatusOK {
		t.Fatalf("GET: %d", code)
	}
	if got.Minutes != 60 || got.DefaultMinutes != 60 || got.Overridden || !got.WorkersEnabled {
		t.Fatalf("GET default = %+v", got)
	}

	code, got = requeueWindowCall(t, router, http.MethodPut, admin, `{"minutes": 240}`)
	if code != http.StatusOK || got.Minutes != 240 || !got.Overridden {
		t.Fatalf("PUT 240: %d %+v", code, got)
	}
	if d, _ := worker.StuckRequeueAfter(); d.Minutes() != 240 {
		t.Fatalf("worker window = %s after PUT 240", d)
	}

	for _, bad := range []string{`{"minutes": 0}`, `{"minutes": 20000}`, `not json`} {
		if code, _ := requeueWindowCall(t, router, http.MethodPut, admin, bad); code != http.StatusBadRequest {
			t.Errorf("PUT %s: %d, want 400", bad, code)
		}
	}
	if d, _ := worker.StuckRequeueAfter(); d.Minutes() != 240 {
		t.Fatalf("a refused PUT changed the window to %s", d)
	}

	code, got = requeueWindowCall(t, router, http.MethodDelete, admin, "")
	if code != http.StatusOK || got.Minutes != 60 || got.Overridden {
		t.Fatalf("DELETE: %d %+v", code, got)
	}

	if n := countAuditEvents(t, router, admin, "upload_requeue_window_changed"); n != 2 {
		t.Errorf("audit events = %d, want 2 (one set, one reset)", n)
	}
}

func TestAdminUploadRequeueWindowNonAdmin(t *testing.T) {
	router := setupRequeueWindowRouter(t, true)
	user := registerAndGetToken(t, router, "user@test.com", "password123", "Plain", "User")

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if code, _ := requeueWindowCall(t, router, method, user, `{"minutes": 120}`); code != http.StatusForbidden {
			t.Errorf("%s as non-admin: %d, want 403", method, code)
		}
	}
}

// A reader instance has no upload worker: it reports that and refuses changes.
func TestAdminUploadRequeueWindowReader(t *testing.T) {
	router := setupRequeueWindowRouter(t, false)
	admin := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	code, got := requeueWindowCall(t, router, http.MethodGet, admin, "")
	if code != http.StatusOK || got.WorkersEnabled {
		t.Fatalf("GET on a reader: %d %+v", code, got)
	}
	if code, _ := requeueWindowCall(t, router, http.MethodPut, admin, `{"minutes": 120}`); code != http.StatusConflict {
		t.Errorf("PUT on a reader: %d, want 409", code)
	}
	if code, _ := requeueWindowCall(t, router, http.MethodDelete, admin, ""); code != http.StatusConflict {
		t.Errorf("DELETE on a reader: %d, want 409", code)
	}
}
