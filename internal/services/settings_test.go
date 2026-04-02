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
