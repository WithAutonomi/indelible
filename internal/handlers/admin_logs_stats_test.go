package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/services"
)

// V2-319: aggregate stats endpoints for audit, system, and config logs.

func TestAuditStats_EmptyTableShape(t *testing.T) {
	router, _ := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit/stats", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("stats: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, w.Body.String())
	}
	if resp["total_entries"].(float64) != 0 {
		t.Errorf("empty table total = %v, want 0", resp["total_entries"])
	}
	if _, ok := resp["by_day"]; !ok {
		t.Error("by_day missing from response")
	}
	days := resp["by_day"].([]any)
	if len(days) != 30 {
		t.Errorf("by_day length = %d, want 30 (padded window)", len(days))
	}
}

func TestAuditStats_PopulatedTableBucketsCorrectly(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	logSvc := services.NewLogService(db)

	uid := int64(1)
	mustWriteAudit(t, logSvc, "login", "info", &uid, "a")
	mustWriteAudit(t, logSvc, "login", "info", &uid, "b")
	mustWriteAudit(t, logSvc, "login_failed", "warn", &uid, "c")
	mustWriteAudit(t, logSvc, "permission_denied", "error", &uid, "d")

	req := httptest.NewRequest("GET", "/api/v2/admin/logs/audit/stats", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("stats: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["total_entries"].(float64) != 4 {
		t.Errorf("total_entries = %v, want 4", resp["total_entries"])
	}
	sev := resp["by_severity"].(map[string]any)
	if sev["info"].(float64) != 2 || sev["warn"].(float64) != 1 || sev["error"].(float64) != 1 {
		t.Errorf("by_severity = %v, want info=2 warn=1 error=1", sev)
	}
	ev := resp["by_event_type"].(map[string]any)
	if ev["login"].(float64) != 2 || ev["login_failed"].(float64) != 1 || ev["permission_denied"].(float64) != 1 {
		t.Errorf("by_event_type = %v", ev)
	}

	// by_day's last bucket is today and should have all 4 rows.
	days := resp["by_day"].([]any)
	last := days[len(days)-1].(map[string]any)
	today := time.Now().UTC().Format("2006-01-02")
	if last["date"].(string) != today {
		t.Errorf("by_day last date = %v, want %v", last["date"], today)
	}
	if last["count"].(float64) != 4 {
		t.Errorf("today's bucket count = %v, want 4", last["count"])
	}
}

func TestSystemStats_PopulatedTable(t *testing.T) {
	router, db := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")
	logSvc := services.NewLogService(db)

	mustWriteSystem(t, logSvc, "info", "worker", "ok", "")
	mustWriteSystem(t, logSvc, "info", "worker", "ok", "")
	mustWriteSystem(t, logSvc, "error", "auth", "fail", "")

	req := httptest.NewRequest("GET", "/api/v2/admin/logs/system/stats", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("system stats: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_entries"].(float64) != 3 {
		t.Errorf("total = %v, want 3", resp["total_entries"])
	}
	lv := resp["by_level"].(map[string]any)
	if lv["info"].(float64) != 2 || lv["error"].(float64) != 1 {
		t.Errorf("by_level = %v", lv)
	}
	cm := resp["by_component"].(map[string]any)
	if cm["worker"].(float64) != 2 || cm["auth"].(float64) != 1 {
		t.Errorf("by_component = %v", cm)
	}
}

func TestConfigAuditStats_AfterPatch(t *testing.T) {
	router, _ := setupRouterWithDB(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	body, _ := json.Marshal(map[string]string{"scim_enabled": "true"})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/v2/admin/logs/config/stats", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("config stats: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_entries"].(float64) != 1 {
		t.Errorf("total = %v, want 1", resp["total_entries"])
	}
	sk := resp["by_setting_key"].(map[string]any)
	if sk["scim_enabled"].(float64) != 1 {
		t.Errorf("by_setting_key = %v", sk)
	}
}
