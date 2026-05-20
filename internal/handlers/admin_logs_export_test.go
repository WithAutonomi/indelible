package handlers_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// setupRouterWithDB mirrors setupTestRouter (auth_test.go) but exposes the
// underlying *database.DB so the test can seed audit/system rows directly via
// the service layer. V2-318 needs this because audit_log writes from the rest
// of the codebase don't land here until V2-314/V2-315 ship.
func setupRouterWithDB(t *testing.T) (http.Handler, *database.DB) {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	db := dbtest.OpenDB(t)
	return handlers.NewRouter(cfg, db, nil), db
}

func mustWriteAudit(t *testing.T, svc *services.LogService, eventType, severity string, userID *int64, detail string) {
	t.Helper()
	if err := svc.WriteAudit(eventType, severity, userID, detail, "", ""); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
}

func mustWriteSystem(t *testing.T, svc *services.LogService, level, component, message, detail string) {
	t.Helper()
	if err := svc.WriteSystem(level, component, message, detail); err != nil {
		t.Fatalf("WriteSystem: %v", err)
	}
}

// V2-318: severity filter on /admin/logs/audit + JSONL export endpoints
// for audit / system / user / config logs.

func TestAuditLogs_SeverityFilterNarrowsResults(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	logSvc := services.NewLogService(db)
	uid := int64(1)
	mustWriteAudit(t, logSvc, "login", "info", &uid, "info entry")
	mustWriteAudit(t, logSvc, "login_failed", "warn", &uid, "warn entry")
	mustWriteAudit(t, logSvc, "permission_denied", "error", &uid, "error entry")

	// All three returned without a filter.
	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if int(resp["total"].(float64)) != 3 {
		t.Fatalf("baseline total = %v, want 3", resp["total"])
	}

	// severity=warn returns just one row.
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/audit?severity=warn", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if int(resp["total"].(float64)) != 1 {
		t.Errorf("severity=warn total = %v, want 1", resp["total"])
	}
	entries := resp["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["severity"] != "warn" {
		t.Errorf("severity=warn returned wrong rows: %v", entries)
	}

	// severity=bogus is silently ignored (matches all).
	req = httptest.NewRequest("GET", "/api/v2/admin/logs/audit?severity=bogus", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if int(resp["total"].(float64)) != 3 {
		t.Errorf("severity=bogus should match all, got total = %v", resp["total"])
	}
}

func TestAuditLogs_ExportProducesNDJSON(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	logSvc := services.NewLogService(db)
	uid := int64(1)
	mustWriteAudit(t, logSvc, "login", "info", &uid, "row 1")
	mustWriteAudit(t, logSvc, "login", "info", &uid, "row 2")
	mustWriteAudit(t, logSvc, "logout", "info", &uid, "row 3")

	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit/export", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("export: got %d, body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("Content-Type = %q, want application/x-ndjson", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="audit-`) {
		t.Errorf("Content-Disposition = %q", cd)
	}

	// One JSON object per line.
	sc := bufio.NewScanner(bytes.NewReader(w.Body.Bytes()))
	count := 0
	for sc.Scan() {
		var obj map[string]any
		if err := json.Unmarshal(sc.Bytes(), &obj); err != nil {
			t.Fatalf("line %d not valid JSON: %v\n%s", count+1, err, sc.Text())
		}
		count++
	}
	if count != 3 {
		t.Errorf("export rows = %d, want 3", count)
	}
}

func TestAuditLogs_ExportHonorsFilters(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	logSvc := services.NewLogService(db)
	uid := int64(1)
	mustWriteAudit(t, logSvc, "login", "info", &uid, "good")
	mustWriteAudit(t, logSvc, "login_failed", "warn", &uid, "narrowed")
	mustWriteAudit(t, logSvc, "logout", "info", &uid, "good")

	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit/export?event_type=login_failed", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("filtered export: got %d", w.Code)
	}
	lines := strings.Split(strings.TrimRight(w.Body.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("filtered export rows = %d, want 1; body: %q", len(lines), w.Body.String())
	}
	var row map[string]any
	json.Unmarshal([]byte(lines[0]), &row)
	if row["event_type"] != "login_failed" {
		t.Errorf("row event_type = %v, want login_failed", row["event_type"])
	}
}

func TestSystemLogs_ExportProducesNDJSON(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	logSvc := services.NewLogService(db)
	mustWriteSystem(t, logSvc, "info", "worker", "did a thing", "")
	mustWriteSystem(t, logSvc, "error", "worker", "broke a thing", "stack trace")

	req := httptest.NewRequest("GET", "/api/v2/admin/logs/system/export?level=error", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("system export: got %d", w.Code)
	}
	lines := strings.Split(strings.TrimRight(w.Body.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("level=error rows = %d, want 1", len(lines))
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("row not valid JSON: %v", err)
	}
	if row["level"] != "error" {
		t.Errorf("row level = %v, want error", row["level"])
	}
}

func TestConfigAudit_ExportAfterSettingsPatch(t *testing.T) {
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{"scim_enabled": "true"})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch settings: got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/v2/admin/logs/config/export", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("config export: got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"setting_key":"scim_enabled"`) {
		t.Errorf("config export missing the scim_enabled row: %s", w.Body.String())
	}
}
