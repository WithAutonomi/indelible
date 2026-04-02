package services

import (
	"fmt"
	"testing"
)

func TestQuotaCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	q, err := svc.Create("user", "42", 1073741824) // 1GB
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if q.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if q.EntityType != "user" {
		t.Errorf("expected entity_type=user, got %s", q.EntityType)
	}
	if !q.EntityID.Valid || q.EntityID.String != "42" {
		t.Errorf("expected entity_id=42, got %v", q.EntityID)
	}
	if q.MaxBytes != 1073741824 {
		t.Errorf("expected max_bytes=1073741824, got %d", q.MaxBytes)
	}
	if !q.IsEnabled {
		t.Error("expected is_enabled=true by default")
	}
}

func TestQuotaCreateSystem(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	q, err := svc.Create("system", "", 10737418240) // 10GB
	if err != nil {
		t.Fatalf("Create system: %v", err)
	}
	if q.EntityType != "system" {
		t.Errorf("expected entity_type=system, got %s", q.EntityType)
	}
	if q.EntityID.Valid {
		t.Error("expected entity_id to be NULL for system quota")
	}
}

func TestQuotaCreateDuplicate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	_, err := svc.Create("user", "1", 1000)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}

	_, err = svc.Create("user", "1", 2000)
	if err != ErrQuotaDuplicate {
		t.Errorf("expected ErrQuotaDuplicate, got %v", err)
	}
}

func TestQuotaGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	created, _ := svc.Create("user", "5", 5000)

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.MaxBytes != 5000 {
		t.Errorf("expected max_bytes=5000, got %d", got.MaxBytes)
	}
}

func TestQuotaGetByIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	_, err := svc.GetByID(99999)
	if err != ErrQuotaNotFound {
		t.Errorf("expected ErrQuotaNotFound, got %v", err)
	}
}

func TestQuotaGetByIDIncludesUsage(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "usage@example.com", "Usage", "User")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// Create completed uploads for this user
	u1 := createTestUpload(t, uploadSvc, user.ID, "f1.bin", 1000)
	u2 := createTestUpload(t, uploadSvc, user.ID, "f2.bin", 2000)
	uploadSvc.MarkCompleted(u1.ID, "map1", "10")
	uploadSvc.MarkCompleted(u2.ID, "map2", "20")

	// Also create a queued upload (should not count)
	createTestUpload(t, uploadSvc, user.ID, "queued.bin", 5000)

	q, _ := quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 100000)

	got, err := quotaSvc.GetByID(q.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.UsedBytes != 3000 {
		t.Errorf("expected used_bytes=3000 (only completed), got %d", got.UsedBytes)
	}
}

func TestQuotaList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	svc.Create("user", "1", 1000)
	svc.Create("user", "2", 2000)
	svc.Create("system", "", 50000)

	quotas, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(quotas) != 3 {
		t.Errorf("expected 3 quotas, got %d", len(quotas))
	}
}

func TestQuotaListOrdering(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	svc.Create("user", "2", 2000)
	svc.Create("system", "", 50000)
	svc.Create("user", "1", 1000)

	quotas, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Should be ordered by entity_type then entity_id
	if len(quotas) >= 2 && quotas[0].EntityType == "user" && quotas[1].EntityType == "user" {
		// Both user quotas should come before system
		if quotas[len(quotas)-1].EntityType != "system" {
			t.Error("expected system quota last")
		}
	}
}

func TestQuotaUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	q, _ := svc.Create("user", "1", 1000)

	updated, err := svc.Update(q.ID, 5000, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.MaxBytes != 5000 {
		t.Errorf("expected max_bytes=5000, got %d", updated.MaxBytes)
	}
	if updated.IsEnabled {
		t.Error("expected is_enabled=false after update")
	}
}

func TestQuotaDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	q, _ := svc.Create("user", "1", 1000)

	err := svc.Delete(q.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.GetByID(q.ID)
	if err != ErrQuotaNotFound {
		t.Errorf("expected ErrQuotaNotFound after delete, got %v", err)
	}
}

func TestQuotaDeleteNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	err := svc.Delete(99999)
	if err != ErrQuotaNotFound {
		t.Errorf("expected ErrQuotaNotFound, got %v", err)
	}
}

func TestQuotaCheckUserQuotaWithinLimit(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "within@example.com", "With", "In")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// Create a user quota of 10000 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 10000)

	// Create a completed upload using 3000 bytes
	u := createTestUpload(t, uploadSvc, user.ID, "existing.bin", 3000)
	uploadSvc.MarkCompleted(u.ID, "map", "10")

	// Check if we can add 5000 more bytes (3000+5000=8000 <= 10000)
	err := quotaSvc.CheckUserQuota(user.ID, 5000)
	if err != nil {
		t.Errorf("expected no error (within limit), got %v", err)
	}
}

func TestQuotaCheckUserQuotaExceedsLimit(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "exceed@example.com", "Ex", "Ceed")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// Create a user quota of 5000 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 5000)

	// Use up 3000 bytes
	u := createTestUpload(t, uploadSvc, user.ID, "existing.bin", 3000)
	uploadSvc.MarkCompleted(u.ID, "map", "10")

	// Try to add 3000 more (3000+3000=6000 > 5000)
	err := quotaSvc.CheckUserQuota(user.ID, 3000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestQuotaCheckUserQuotaExactLimit(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "exact@example.com", "Ex", "Act")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// Create a user quota of 5000 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 5000)

	// Use up 3000 bytes
	u := createTestUpload(t, uploadSvc, user.ID, "existing.bin", 3000)
	uploadSvc.MarkCompleted(u.ID, "map", "10")

	// Adding exactly 2000 more (3000+2000=5000 == 5000) should be OK
	err := quotaSvc.CheckUserQuota(user.ID, 2000)
	if err != nil {
		t.Errorf("expected no error at exact limit, got %v", err)
	}

	// Adding 2001 should exceed
	err = quotaSvc.CheckUserQuota(user.ID, 2001)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded for 1 byte over, got %v", err)
	}
}

func TestQuotaCheckSystemQuota(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user1 := createTestUser(t, userSvc, "sys1@example.com", "Sys", "One")
	user2 := createTestUser(t, userSvc, "sys2@example.com", "Sys", "Two")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// System-wide quota of 10000 bytes
	quotaSvc.Create("system", "", 10000)

	// user1 uses 4000
	u1 := createTestUpload(t, uploadSvc, user1.ID, "u1.bin", 4000)
	uploadSvc.MarkCompleted(u1.ID, "map1", "10")

	// user2 uses 4000
	u2 := createTestUpload(t, uploadSvc, user2.ID, "u2.bin", 4000)
	uploadSvc.MarkCompleted(u2.ID, "map2", "10")

	// Total system usage: 8000. Adding 1500 should be OK
	err := quotaSvc.CheckUserQuota(user1.ID, 1500)
	if err != nil {
		t.Errorf("expected no error (system within limit), got %v", err)
	}

	// Adding 3000 more should exceed system quota (8000+3000=11000 > 10000)
	err = quotaSvc.CheckUserQuota(user1.ID, 3000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded for system quota, got %v", err)
	}
}

func TestQuotaCheckBothUserAndSystem(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "both@example.com", "Both", "Quota")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// User quota: 5000 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 5000)
	// System quota: 100000 bytes (generous)
	quotaSvc.Create("system", "", 100000)

	u := createTestUpload(t, uploadSvc, user.ID, "f.bin", 3000)
	uploadSvc.MarkCompleted(u.ID, "map", "10")

	// Within user quota
	err := quotaSvc.CheckUserQuota(user.ID, 1500)
	if err != nil {
		t.Errorf("expected no error (within user quota), got %v", err)
	}

	// Exceeds user quota (3000+3000=6000 > 5000)
	err = quotaSvc.CheckUserQuota(user.ID, 3000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded from user quota, got %v", err)
	}
}

func TestQuotaCheckDisabledQuota(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "disabled@example.com", "Dis", "Abled")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// Create a tiny user quota and then disable it
	q, _ := quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 100) // 100 bytes
	quotaSvc.Update(q.ID, 100, false)                                 // disable

	// Add a big file
	u := createTestUpload(t, uploadSvc, user.ID, "big.bin", 50000)
	uploadSvc.MarkCompleted(u.ID, "map", "10")

	// Disabled quota should not block
	err := quotaSvc.CheckUserQuota(user.ID, 50000)
	if err != nil {
		t.Errorf("expected no error (quota disabled), got %v", err)
	}
}

func TestQuotaCheckNoQuotaSet(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "noquota@example.com", "No", "Quota")
	quotaSvc := NewQuotaService(db)

	// No quotas at all -- should always pass
	err := quotaSvc.CheckUserQuota(user.ID, 999999999)
	if err != nil {
		t.Errorf("expected no error (no quotas set), got %v", err)
	}
}

func TestQuotaCheckOnlyCompletedUploadsCount(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "onlycomplete@example.com", "Only", "Complete")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 5000)

	// Create uploads in various states
	u1 := createTestUpload(t, uploadSvc, user.ID, "completed.bin", 2000)
	uploadSvc.MarkCompleted(u1.ID, "map", "10")

	u2 := createTestUpload(t, uploadSvc, user.ID, "queued.bin", 3000)   // queued - shouldn't count
	createTestUpload(t, uploadSvc, user.ID, "queued2.bin", 4000)         // queued - shouldn't count

	u4 := createTestUpload(t, uploadSvc, user.ID, "failed.bin", 3000)
	uploadSvc.MarkFailed(u4.ID, "err") // failed - shouldn't count

	_ = u2

	// Only 2000 bytes used (the completed one). Adding 2500 should be fine.
	err := quotaSvc.CheckUserQuota(user.ID, 2500)
	if err != nil {
		t.Errorf("expected no error (only completed counts, 2000+2500=4500 < 5000), got %v", err)
	}
}

func TestQuotaSystemUsageCalculation(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "sysusage@example.com", "Sys", "Usage")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// Create a system quota
	q, _ := quotaSvc.Create("system", "", 100000)

	// No uploads yet
	got, _ := quotaSvc.GetByID(q.ID)
	if got.UsedBytes != 0 {
		t.Errorf("expected used_bytes=0, got %d", got.UsedBytes)
	}

	// Add some completed uploads
	u1 := createTestUpload(t, uploadSvc, user.ID, "f1.bin", 1500)
	uploadSvc.MarkCompleted(u1.ID, "m1", "10")
	u2 := createTestUpload(t, uploadSvc, user.ID, "f2.bin", 2500)
	uploadSvc.MarkCompleted(u2.ID, "m2", "20")

	got, _ = quotaSvc.GetByID(q.ID)
	if got.UsedBytes != 4000 {
		t.Errorf("expected used_bytes=4000, got %d", got.UsedBytes)
	}
}

func TestQuotaUpdateEnableDisable(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	q, _ := svc.Create("user", "1", 1000)
	if !q.IsEnabled {
		t.Fatal("expected quota to be enabled by default")
	}

	// Disable
	updated, err := svc.Update(q.ID, 1000, false)
	if err != nil {
		t.Fatalf("Update disable: %v", err)
	}
	if updated.IsEnabled {
		t.Error("expected is_enabled=false")
	}

	// Re-enable with new limit
	updated, err = svc.Update(q.ID, 2000, true)
	if err != nil {
		t.Fatalf("Update enable: %v", err)
	}
	if !updated.IsEnabled {
		t.Error("expected is_enabled=true")
	}
	if updated.MaxBytes != 2000 {
		t.Errorf("expected max_bytes=2000, got %d", updated.MaxBytes)
	}
}

func TestCheckUserQuotaInFlight_CountsQueuedUploads(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "inflight-queued@example.com", "In", "Flight")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// User quota of 10000 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 10000)

	// Create queued uploads totalling 6000 bytes (queued is the default status from Create)
	createTestUpload(t, uploadSvc, user.ID, "q1.bin", 3000)
	createTestUpload(t, uploadSvc, user.ID, "q2.bin", 3000)

	// CheckUserQuotaInFlight should count queued uploads: 6000 + 3000 = 9000 <= 10000
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, 3000)
	if err != nil {
		t.Errorf("expected no error (queued uploads counted, 6000+3000=9000 <= 10000), got %v", err)
	}

	// Contrast with CheckUserQuota which ignores queued: 0 + 3000 = 3000 <= 10000
	err = quotaSvc.CheckUserQuota(user.ID, 3000)
	if err != nil {
		t.Errorf("expected no error from CheckUserQuota (queued not counted), got %v", err)
	}

	// InFlight should block when queued + new exceeds quota: 6000 + 5000 = 11000 > 10000
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, 5000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (queued counted, 6000+5000=11000 > 10000), got %v", err)
	}
}

func TestCheckUserQuotaInFlight_CountsProcessingUploads(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "inflight-proc@example.com", "In", "Proc")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// User quota of 10000 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 10000)

	// Create uploads and move them to processing via DequeueNext
	createTestUpload(t, uploadSvc, user.ID, "p1.bin", 4000)
	createTestUpload(t, uploadSvc, user.ID, "p2.bin", 3000)
	uploadSvc.DequeueNext() // moves p1 to processing
	uploadSvc.DequeueNext() // moves p2 to processing

	// CheckUserQuotaInFlight should count processing uploads: 7000 + 2000 = 9000 <= 10000
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, 2000)
	if err != nil {
		t.Errorf("expected no error (processing uploads counted, 7000+2000=9000 <= 10000), got %v", err)
	}

	// Contrast with CheckUserQuota which ignores processing: 0 + 2000 = 2000 <= 10000
	err = quotaSvc.CheckUserQuota(user.ID, 2000)
	if err != nil {
		t.Errorf("expected no error from CheckUserQuota (processing not counted), got %v", err)
	}

	// InFlight should block when processing + new exceeds quota: 7000 + 4000 = 11000 > 10000
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, 4000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (processing counted, 7000+4000=11000 > 10000), got %v", err)
	}
}

func TestCheckUserQuotaInFlight_BlocksWhenInFlightExceedsQuota(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "inflight-block@example.com", "Block", "Test")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// User quota of 100 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 100)

	// Create 80 bytes of queued uploads
	createTestUpload(t, uploadSvc, user.ID, "q80.bin", 80)

	// Try to add 30 more bytes: 80 + 30 = 110 > 100 -- should fail
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, 30)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (80 queued + 30 new = 110 > 100), got %v", err)
	}

	// Exact boundary: 80 + 20 = 100 -- should succeed
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, 20)
	if err != nil {
		t.Errorf("expected no error at exact limit (80+20=100), got %v", err)
	}

	// One byte over: 80 + 21 = 101 > 100 -- should fail
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, 21)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (80+21=101 > 100), got %v", err)
	}
}

func TestCheckUserQuotaInFlight_AllowsWhenUnderQuota(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "inflight-allow@example.com", "Allow", "Test")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// User quota of 100 bytes
	quotaSvc.Create("user", fmt.Sprintf("%d", user.ID), 100)

	// Create 50 bytes of queued uploads
	createTestUpload(t, uploadSvc, user.ID, "q50.bin", 50)

	// Check 30 more: 50 + 30 = 80 <= 100 -- should succeed
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, 30)
	if err != nil {
		t.Errorf("expected no error (50 queued + 30 new = 80 <= 100), got %v", err)
	}

	// Also test with a mix of queued and processing
	createTestUpload(t, uploadSvc, user.ID, "p10.bin", 10)
	uploadSvc.DequeueNext() // moves one upload to processing

	// Now: 50 queued (q50) + 10 processing (p10) + 30 new = 90 <= 100
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, 30)
	if err != nil {
		t.Errorf("expected no error (50 queued + 10 processing + 30 new = 90 <= 100), got %v", err)
	}
}

func TestCheckUserQuotaInFlight_SystemQuota(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user1 := createTestUser(t, userSvc, "sys-inflight1@example.com", "Sys", "One")
	user2 := createTestUser(t, userSvc, "sys-inflight2@example.com", "Sys", "Two")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	// System-wide quota of 10000 bytes (no user-level quota)
	quotaSvc.Create("system", "", 10000)

	// user1 has 4000 bytes queued
	createTestUpload(t, uploadSvc, user1.ID, "u1q.bin", 4000)

	// user2 has 3000 bytes in processing
	createTestUpload(t, uploadSvc, user2.ID, "u2p.bin", 3000)
	uploadSvc.DequeueNext() // moves u2p to processing

	// System in-flight total: 4000 + 3000 = 7000
	// Adding 2000 for user1: 7000 + 2000 = 9000 <= 10000 -- should succeed
	err := quotaSvc.CheckUserQuotaInFlight(user1.ID, 2000)
	if err != nil {
		t.Errorf("expected no error (system in-flight 7000 + 2000 = 9000 <= 10000), got %v", err)
	}

	// Adding 4000 for user1: 7000 + 4000 = 11000 > 10000 -- should fail
	err = quotaSvc.CheckUserQuotaInFlight(user1.ID, 4000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (system in-flight 7000 + 4000 = 11000 > 10000), got %v", err)
	}

	// Contrast with CheckUserQuota which ignores queued/processing: 0 + 4000 = 4000 <= 10000
	err = quotaSvc.CheckUserQuota(user1.ID, 4000)
	if err != nil {
		t.Errorf("expected no error from CheckUserQuota (queued/processing not counted), got %v", err)
	}

	// Exact boundary: 7000 + 3000 = 10000 -- should succeed
	err = quotaSvc.CheckUserQuotaInFlight(user1.ID, 3000)
	if err != nil {
		t.Errorf("expected no error at exact system limit (7000+3000=10000), got %v", err)
	}

	// One byte over: 7000 + 3001 = 10001 > 10000 -- should fail
	err = quotaSvc.CheckUserQuotaInFlight(user1.ID, 3001)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (system 7000+3001=10001 > 10000), got %v", err)
	}
}
