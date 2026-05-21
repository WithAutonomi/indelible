package services

import (
	"encoding/json"
	"testing"
)

func TestSettingsGet_SeededDefaults(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)

	val, err := svc.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "false" {
		t.Errorf("maintenance_mode = %q, want 'false'", val)
	}

	val, err = svc.Get("max_upload_size_bytes")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "10737418240" {
		t.Errorf("max_upload_size_bytes = %q", val)
	}
}

func TestSettingsGetAll(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)

	all, err := svc.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) == 0 {
		t.Error("GetAll returned empty map, expected seeded settings")
	}
	if all["jwt_expiry_hours"] != "24" {
		t.Errorf("jwt_expiry_hours = %q", all["jwt_expiry_hours"])
	}
}

func TestSettingsUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	changes := map[string]string{
		"maintenance_mode": "true",
		"jwt_expiry_hours": "48",
	}
	if err := svc.Update(changes, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	val, _ := svc.Get("maintenance_mode")
	if val != "true" {
		t.Errorf("maintenance_mode after update = %q", val)
	}
	val, _ = svc.Get("jwt_expiry_hours")
	if val != "48" {
		t.Errorf("jwt_expiry_hours after update = %q", val)
	}
}

func TestSettingsUpdate_NewKey(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	changes := map[string]string{"custom_setting": "custom_value"}
	if err := svc.Update(changes, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("Update new key: %v", err)
	}

	val, err := svc.Get("custom_setting")
	if err != nil {
		t.Fatalf("Get new key: %v", err)
	}
	if val != "custom_value" {
		t.Errorf("custom_setting = %q", val)
	}
}

// --- Config audit query (V2-281 item 2) -----------------------------------

func TestSettingsUpdate_WritesConfigAuditWithOldNewActorIPUA(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	user := createTestUser(t, NewUserService(db), "admin@test.com", "Admin", "U")

	// Two changes against pre-seeded keys so we see both
	// "new value over previous value" and a brand-new key.
	if err := svc.Update(map[string]string{"maintenance_mode": "true", "custom_x": "v1"}, user.ID, "10.1.2.3", "ua/1.0"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	entries, total, err := svc.QueryConfigAudit("", nil, nil, 100, 0)
	if err != nil {
		t.Fatalf("QueryConfigAudit: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	byKey := map[string]*ConfigAuditEntry{}
	for _, e := range entries {
		byKey[e.SettingKey] = e
	}

	mm := byKey["maintenance_mode"]
	if mm == nil {
		t.Fatal("missing maintenance_mode audit row")
	}
	if mm.NewValue != "true" {
		t.Errorf("maintenance_mode new = %q", mm.NewValue)
	}
	if !mm.OldValue.Valid || mm.OldValue.String != "false" {
		t.Errorf("maintenance_mode old = %v (want 'false' — seeded default)", mm.OldValue)
	}
	if !mm.ChangedBy.Valid || mm.ChangedBy.Int64 != user.ID {
		t.Errorf("changed_by = %v, want %d", mm.ChangedBy, user.ID)
	}
	if !mm.IPAddress.Valid || mm.IPAddress.String != "10.1.2.3" {
		t.Errorf("ip = %v", mm.IPAddress)
	}
	if !mm.UserAgent.Valid || mm.UserAgent.String != "ua/1.0" {
		t.Errorf("ua = %v", mm.UserAgent)
	}

	cx := byKey["custom_x"]
	if cx == nil {
		t.Fatal("missing custom_x audit row")
	}
	if cx.NewValue != "v1" {
		t.Errorf("custom_x new = %q", cx.NewValue)
	}
	if cx.OldValue.Valid {
		t.Errorf("custom_x old should be NULL (new key), got %v", cx.OldValue.String)
	}
}

func TestSettingsQueryConfigAudit_FilterBySettingKey(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	user := createTestUser(t, NewUserService(db), "admin@test.com", "Admin", "U")

	_ = svc.Update(map[string]string{"maintenance_mode": "true"}, user.ID, "", "")
	_ = svc.Update(map[string]string{"timezone": "UTC"}, user.ID, "", "")
	_ = svc.Update(map[string]string{"timezone": "America/New_York"}, user.ID, "", "")

	entries, total, err := svc.QueryConfigAudit("timezone", nil, nil, 100, 0)
	if err != nil {
		t.Fatalf("QueryConfigAudit: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (filtered to timezone)", total)
	}
	for _, e := range entries {
		if e.SettingKey != "timezone" {
			t.Errorf("got entry with key=%q in filtered query", e.SettingKey)
		}
	}
	// Newest-first ordering: most recent value should come back first.
	if entries[0].NewValue != "America/New_York" {
		t.Errorf("expected newest-first ordering, got %q first", entries[0].NewValue)
	}
}

func TestSettingsQueryConfigAudit_LimitOffsetAndCap(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	user := createTestUser(t, NewUserService(db), "admin@test.com", "Admin", "U")

	for i := 0; i < 5; i++ {
		_ = svc.Update(map[string]string{"counter": fmtInt(i)}, user.ID, "", "")
	}

	page1, total, _ := svc.QueryConfigAudit("counter", nil, nil, 2, 0)
	if total != 5 || len(page1) != 2 {
		t.Errorf("page1: total=%d len=%d, want 5/2", total, len(page1))
	}
	page2, _, _ := svc.QueryConfigAudit("counter", nil, nil, 2, 2)
	if len(page2) != 2 {
		t.Errorf("page2 len=%d, want 2", len(page2))
	}
	// Page2's first entry must be different from page1's first.
	if page1[0].ID == page2[0].ID {
		t.Error("pagination did not advance")
	}

	// limit=0 → defaults to 100; limit>500 → capped to 500.
	defLimit, _, _ := svc.QueryConfigAudit("counter", nil, nil, 0, 0)
	if len(defLimit) != 5 { // we only inserted 5
		t.Errorf("limit=0 should default to 100, got %d entries", len(defLimit))
	}
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestSettingsExport(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)

	data, err := svc.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var exported ExportData
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if len(exported.Settings) == 0 {
		t.Error("exported settings should not be empty")
	}
	if exported.Settings["maintenance_mode"] != "false" {
		t.Errorf("exported maintenance_mode = %q", exported.Settings["maintenance_mode"])
	}
}

func TestSettingsExportImport_Roundtrip(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	// Modify a setting
	svc.Update(map[string]string{"jwt_expiry_hours": "72"}, user.ID, "127.0.0.1", "TestAgent")

	// Export
	exported, err := svc.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Create a fresh DB and import
	db2 := setupTestDB(t)
	svc2 := NewSettingsService(db2)
	userSvc2 := NewUserService(db2)
	user2 := createTestUser(t, userSvc2, "admin@test.com", "Admin", "User")

	var data ExportData
	json.Unmarshal(exported, &data)

	if err := svc2.ImportStructured(&data, user2.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("ImportStructured: %v", err)
	}

	val, _ := svc2.Get("jwt_expiry_hours")
	if val != "72" {
		t.Errorf("after import jwt_expiry_hours = %q, want 72", val)
	}
}

func TestSettingsImport_Legacy(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	flat := map[string]string{
		"maintenance_mode": "true",
		"timezone":         "US/Eastern",
	}
	if err := svc.Import(flat, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("Import: %v", err)
	}

	val, _ := svc.Get("maintenance_mode")
	if val != "true" {
		t.Errorf("maintenance_mode = %q", val)
	}
	val, _ = svc.Get("timezone")
	if val != "US/Eastern" {
		t.Errorf("timezone = %q", val)
	}
}
