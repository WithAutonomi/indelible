package services

import (
	"fmt"
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
)

// seedUserID creates a user and returns its id as the string entity_id a quota
// references. Quota creation validates that user/group entities exist
// (V2-396), so CRUD tests must reference a real user rather than a literal id.
func seedUserID(t *testing.T, db *database.DB, email string) string {
	t.Helper()
	u := createTestUser(t, NewUserService(db), email, "T", "U")
	return int64ToString(u.ID)
}

func TestQuotaCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	uid := seedUserID(t, db, "create@example.com")
	q, err := svc.Create("user", uid, 1073741824) // 1GB
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if q.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if q.EntityType != "user" {
		t.Errorf("expected entity_type=user, got %s", q.EntityType)
	}
	if !q.EntityID.Valid || q.EntityID.String != uid {
		t.Errorf("expected entity_id=%s, got %v", uid, q.EntityID)
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

	uid := seedUserID(t, db, "dup@example.com")
	_, err := svc.Create("user", uid, 1000)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}

	_, err = svc.Create("user", uid, 2000)
	if err != ErrQuotaDuplicate {
		t.Errorf("expected ErrQuotaDuplicate, got %v", err)
	}
}

func TestQuotaCreateEntityRequired(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	for _, et := range []string{"user", "group", "department"} {
		if _, err := svc.Create(et, "", 1000); err != ErrQuotaEntityRequired {
			t.Errorf("Create(%q, \"\"): expected ErrQuotaEntityRequired, got %v", et, err)
		}
	}
}

func TestQuotaCreateEntityNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	// A numeric but unknown user id, and a non-numeric id, both reject —
	// otherwise the quota would be silently inert (V2-396).
	for _, id := range []string{"99999", "not-a-number"} {
		if _, err := svc.Create("user", id, 1000); err != ErrQuotaEntityNotFound {
			t.Errorf("Create(user, %q): expected ErrQuotaEntityNotFound, got %v", id, err)
		}
	}
	if _, err := svc.Create("group", "12345", 1000); err != ErrQuotaEntityNotFound {
		t.Errorf("Create(group, unknown): expected ErrQuotaEntityNotFound, got %v", err)
	}
}

func TestQuotaCreateGroupExists(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)
	g, err := NewGroupService(db).Create("engineering", "", "read")
	if err != nil {
		t.Fatalf("group Create: %v", err)
	}
	if _, err := svc.Create("group", int64ToString(g.ID), 1000); err != nil {
		t.Errorf("Create(group, existing): unexpected error %v", err)
	}
}

func TestQuotaCreateDepartmentNotExistenceChecked(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	// A department quota may be set before any token uses that department.
	if _, err := svc.Create("department", "future-dept", 1000); err != nil {
		t.Errorf("Create(department, free-text): unexpected error %v", err)
	}
}

func TestQuotaCreateInvalidEntityType(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	if _, err := svc.Create("bogus", "x", 1000); err != ErrQuotaInvalidEntityType {
		t.Errorf("expected ErrQuotaInvalidEntityType, got %v", err)
	}
}

func TestQuotaGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewQuotaService(db)

	uid := seedUserID(t, db, "getbyid@example.com")
	created, _ := svc.Create("user", uid, 5000)

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

	svc.Create("user", seedUserID(t, db, "list1@example.com"), 1000)
	svc.Create("user", seedUserID(t, db, "list2@example.com"), 2000)
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

	u1 := seedUserID(t, db, "ord1@example.com")
	u2 := seedUserID(t, db, "ord2@example.com")
	svc.Create("user", u2, 2000)
	svc.Create("system", "", 50000)
	svc.Create("user", u1, 1000)

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

	q, _ := svc.Create("user", seedUserID(t, db, "update@example.com"), 1000)

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

	q, _ := svc.Create("user", seedUserID(t, db, "delete@example.com"), 1000)

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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 5000)
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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 3000)
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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 2000)
	if err != nil {
		t.Errorf("expected no error at exact limit, got %v", err)
	}

	// Adding 2001 should exceed
	err = quotaSvc.CheckUserQuota(user.ID, nil, 2001)
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
	err := quotaSvc.CheckUserQuota(user1.ID, nil, 1500)
	if err != nil {
		t.Errorf("expected no error (system within limit), got %v", err)
	}

	// Adding 3000 more should exceed system quota (8000+3000=11000 > 10000)
	err = quotaSvc.CheckUserQuota(user1.ID, nil, 3000)
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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 1500)
	if err != nil {
		t.Errorf("expected no error (within user quota), got %v", err)
	}

	// Exceeds user quota (3000+3000=6000 > 5000)
	err = quotaSvc.CheckUserQuota(user.ID, nil, 3000)
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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 50000)
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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 999999999)
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
	err := quotaSvc.CheckUserQuota(user.ID, nil, 2500)
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

	q, _ := svc.Create("user", seedUserID(t, db, "enabledisable@example.com"), 1000)
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
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 3000)
	if err != nil {
		t.Errorf("expected no error (queued uploads counted, 6000+3000=9000 <= 10000), got %v", err)
	}

	// Contrast with CheckUserQuota which ignores queued: 0 + 3000 = 3000 <= 10000
	err = quotaSvc.CheckUserQuota(user.ID, nil, 3000)
	if err != nil {
		t.Errorf("expected no error from CheckUserQuota (queued not counted), got %v", err)
	}

	// InFlight should block when queued + new exceeds quota: 6000 + 5000 = 11000 > 10000
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 5000)
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
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 2000)
	if err != nil {
		t.Errorf("expected no error (processing uploads counted, 7000+2000=9000 <= 10000), got %v", err)
	}

	// Contrast with CheckUserQuota which ignores processing: 0 + 2000 = 2000 <= 10000
	err = quotaSvc.CheckUserQuota(user.ID, nil, 2000)
	if err != nil {
		t.Errorf("expected no error from CheckUserQuota (processing not counted), got %v", err)
	}

	// InFlight should block when processing + new exceeds quota: 7000 + 4000 = 11000 > 10000
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 4000)
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
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 30)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (80 queued + 30 new = 110 > 100), got %v", err)
	}

	// Exact boundary: 80 + 20 = 100 -- should succeed
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 20)
	if err != nil {
		t.Errorf("expected no error at exact limit (80+20=100), got %v", err)
	}

	// One byte over: 80 + 21 = 101 > 100 -- should fail
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 21)
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
	err := quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 30)
	if err != nil {
		t.Errorf("expected no error (50 queued + 30 new = 80 <= 100), got %v", err)
	}

	// Also test with a mix of queued and processing
	createTestUpload(t, uploadSvc, user.ID, "p10.bin", 10)
	uploadSvc.DequeueNext() // moves one upload to processing

	// Now: 50 queued (q50) + 10 processing (p10) + 30 new = 90 <= 100
	err = quotaSvc.CheckUserQuotaInFlight(user.ID, nil, 30)
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
	err := quotaSvc.CheckUserQuotaInFlight(user1.ID, nil, 2000)
	if err != nil {
		t.Errorf("expected no error (system in-flight 7000 + 2000 = 9000 <= 10000), got %v", err)
	}

	// Adding 4000 for user1: 7000 + 4000 = 11000 > 10000 -- should fail
	err = quotaSvc.CheckUserQuotaInFlight(user1.ID, nil, 4000)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (system in-flight 7000 + 4000 = 11000 > 10000), got %v", err)
	}

	// Contrast with CheckUserQuota which ignores queued/processing: 0 + 4000 = 4000 <= 10000
	err = quotaSvc.CheckUserQuota(user1.ID, nil, 4000)
	if err != nil {
		t.Errorf("expected no error from CheckUserQuota (queued/processing not counted), got %v", err)
	}

	// Exact boundary: 7000 + 3000 = 10000 -- should succeed
	err = quotaSvc.CheckUserQuotaInFlight(user1.ID, nil, 3000)
	if err != nil {
		t.Errorf("expected no error at exact system limit (7000+3000=10000), got %v", err)
	}

	// One byte over: 7000 + 3001 = 10001 > 10000 -- should fail
	err = quotaSvc.CheckUserQuotaInFlight(user1.ID, nil, 3001)
	if err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded (system 7000+3001=10001 > 10000), got %v", err)
	}
}

// --- Group tier --------------------------------------------------------------

// quotaGroupFixture sets up a group with two users (alice + bob), each having
// completed uploads — handy for the aggregate-across-members tests.
type quotaGroupFixture struct {
	alice, bob *User
	groupID    int64
}

func setupGroupFixture(t *testing.T, db *database.DB, aliceBytes, bobBytes int64) quotaGroupFixture {
	t.Helper()
	userSvc := NewUserService(db)
	groupSvc := NewGroupService(db)
	uploadSvc := NewUploadService(db)

	alice := createTestUser(t, userSvc, "alice@example.com", "Alice", "G")
	bob := createTestUser(t, userSvc, "bob@example.com", "Bob", "G")

	g, err := groupSvc.Create("engineering", "", "read")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// addedBy must be a real user FK; alice plays the "admin who added them"
	// role here (V2-273's nullableAddedBy fix lives on a separate branch).
	if err := groupSvc.AddMember(g.ID, alice.ID, alice.ID); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := groupSvc.AddMember(g.ID, bob.ID, alice.ID); err != nil {
		t.Fatalf("add bob: %v", err)
	}

	if aliceBytes > 0 {
		u := createTestUpload(t, uploadSvc, alice.ID, "alice.bin", aliceBytes)
		uploadSvc.MarkCompleted(u.ID, "ma", "1")
	}
	if bobBytes > 0 {
		u := createTestUpload(t, uploadSvc, bob.ID, "bob.bin", bobBytes)
		uploadSvc.MarkCompleted(u.ID, "mb", "1")
	}

	return quotaGroupFixture{alice: alice, bob: bob, groupID: g.ID}
}

func TestQuotaCheckGroupQuotaWithinLimit(t *testing.T) {
	db := setupTestDB(t)
	fx := setupGroupFixture(t, db, 2000, 0)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("group", int64ToString(fx.groupID), 10000)

	// Group total 2000; alice adds 5000 → 7000 <= 10000, fine.
	if err := quotaSvc.CheckUserQuota(fx.alice.ID, nil, 5000); err != nil {
		t.Errorf("within group quota: %v", err)
	}
}

func TestQuotaCheckGroupQuotaExceedsLimit(t *testing.T) {
	db := setupTestDB(t)
	fx := setupGroupFixture(t, db, 3000, 0)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("group", int64ToString(fx.groupID), 5000)

	// 3000 + 3000 = 6000 > 5000, reject.
	if err := quotaSvc.CheckUserQuota(fx.alice.ID, nil, 3000); err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestQuotaCheckGroupQuotaAggregatesAcrossMembers(t *testing.T) {
	db := setupTestDB(t)
	// Alice 4000, Bob 4000 → group total 8000.
	fx := setupGroupFixture(t, db, 4000, 4000)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("group", int64ToString(fx.groupID), 10000)

	// alice adds 1500 → 8000 + 1500 = 9500 <= 10000, fine.
	if err := quotaSvc.CheckUserQuota(fx.alice.ID, nil, 1500); err != nil {
		t.Errorf("aggregate within limit: %v", err)
	}
	// alice adds 3000 → 8000 + 3000 = 11000 > 10000, reject. Note alice's own
	// usage is only 4000, so a user-tier check alone would allow this; the
	// aggregate-across-members rule is what catches it.
	if err := quotaSvc.CheckUserQuota(fx.alice.ID, nil, 3000); err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded from group aggregate, got %v", err)
	}
}

func TestQuotaCheckGroupQuotaIgnoredForNonMember(t *testing.T) {
	db := setupTestDB(t)
	fx := setupGroupFixture(t, db, 0, 0)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("group", int64ToString(fx.groupID), 100) // tiny group quota

	// Outsider has no group membership → group quota doesn't apply.
	outsider := createTestUser(t, NewUserService(db), "outsider@example.com", "Out", "Sider")
	if err := quotaSvc.CheckUserQuota(outsider.ID, nil, 50000); err != nil {
		t.Errorf("non-member should ignore group quota, got %v", err)
	}
}

func TestQuotaCheckGroupQuotaDisabled(t *testing.T) {
	db := setupTestDB(t)
	fx := setupGroupFixture(t, db, 4000, 0)
	quotaSvc := NewQuotaService(db)
	q, _ := quotaSvc.Create("group", int64ToString(fx.groupID), 100)
	quotaSvc.Update(q.ID, 100, false) // disable

	if err := quotaSvc.CheckUserQuota(fx.alice.ID, nil, 50000); err != nil {
		t.Errorf("disabled group quota should not block, got %v", err)
	}
}

// --- Department tier ---------------------------------------------------------

// makeDeptToken issues an API token bound to a department and returns its ID.
func makeDeptToken(t *testing.T, db *database.DB, userID int64, dept string) int64 {
	t.Helper()
	tokSvc := NewTokenService(db)
	_, tok, err := tokSvc.Create(userID, "ci-bot", "", `["write"]`, dept, nil, "", nil)
	if err != nil {
		t.Fatalf("create dept token: %v", err)
	}
	return tok.ID
}

func TestQuotaCheckDepartmentQuotaWithinLimit(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dept-ok@example.com", "D", "U")
	tokenID := makeDeptToken(t, db, user.ID, "engineering")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("department", "engineering", 10000)

	// 3000 used via this token; +5000 → 8000 <= 10000, fine.
	u, _ := uploadSvc.Create(user.ID, &tokenID, "a.bin", "a.bin", 3000, "application/octet-stream", "private", "/tmp/a.bin", nil)
	uploadSvc.MarkCompleted(u.ID, "ma", "1")

	if err := quotaSvc.CheckUserQuota(user.ID, &tokenID, 5000); err != nil {
		t.Errorf("within department quota: %v", err)
	}
}

func TestQuotaCheckDepartmentQuotaExceedsLimit(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dept-x@example.com", "D", "X")
	tokenID := makeDeptToken(t, db, user.ID, "marketing")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("department", "marketing", 5000)

	u, _ := uploadSvc.Create(user.ID, &tokenID, "a.bin", "a.bin", 3000, "application/octet-stream", "private", "/tmp/a.bin", nil)
	uploadSvc.MarkCompleted(u.ID, "ma", "1")

	if err := quotaSvc.CheckUserQuota(user.ID, &tokenID, 3000); err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded from dept, got %v", err)
	}
}

func TestQuotaCheckDepartmentQuotaAggregatesAcrossTokens(t *testing.T) {
	// Two different tokens in the same department — their usage is summed.
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dept-agg@example.com", "D", "A")
	tokA := makeDeptToken(t, db, user.ID, "sales")
	tokB := makeDeptToken(t, db, user.ID, "sales") // same dept, different token
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("department", "sales", 10000)

	// 4000 via tokA, 4000 via tokB → dept total 8000.
	uA, _ := uploadSvc.Create(user.ID, &tokA, "a.bin", "a.bin", 4000, "application/octet-stream", "private", "/tmp/a.bin", nil)
	uploadSvc.MarkCompleted(uA.ID, "ma", "1")
	uB, _ := uploadSvc.Create(user.ID, &tokB, "b.bin", "b.bin", 4000, "application/octet-stream", "private", "/tmp/b.bin", nil)
	uploadSvc.MarkCompleted(uB.ID, "mb", "1")

	// 8000 + 1500 = 9500, fine.
	if err := quotaSvc.CheckUserQuota(user.ID, &tokA, 1500); err != nil {
		t.Errorf("aggregate within: %v", err)
	}
	// 8000 + 3000 = 11000, reject.
	if err := quotaSvc.CheckUserQuota(user.ID, &tokB, 3000); err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded from dept aggregate, got %v", err)
	}
}

func TestQuotaCheckDepartmentQuotaSkippedForNoToken(t *testing.T) {
	// Web-UI uploads have no token → no department to resolve → department
	// quotas don't apply, even tiny ones.
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "no-tok@example.com", "N", "T")
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("department", "engineering", 100)

	if err := quotaSvc.CheckUserQuota(user.ID, nil, 50000); err != nil {
		t.Errorf("nil tokenID should skip dept tier, got %v", err)
	}
}

func TestQuotaCheckDepartmentQuotaSkippedForTokenWithoutDepartment(t *testing.T) {
	// Token exists but has no department set → no dept resolution → skip.
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "no-dept@example.com", "N", "D")
	tokenID := makeDeptToken(t, db, user.ID, "") // empty department
	quotaSvc := NewQuotaService(db)
	quotaSvc.Create("department", "engineering", 100)

	if err := quotaSvc.CheckUserQuota(user.ID, &tokenID, 50000); err != nil {
		t.Errorf("token without dept should skip dept tier, got %v", err)
	}
}

// --- All tiers together ------------------------------------------------------

func TestQuotaCheckAllFourTiersTogether(t *testing.T) {
	// User has: a user quota, membership in a group with a quota, an API token
	// in a department with a quota, plus a system quota. Each tier independently
	// gets exercised; the first one that would fail is the one that rejects.
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	groupSvc := NewGroupService(db)
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	user := createTestUser(t, userSvc, "all-tiers@example.com", "All", "T")
	g, _ := groupSvc.Create("all-tiers-group", "", "read")
	groupSvc.AddMember(g.ID, user.ID, user.ID)
	tokenID := makeDeptToken(t, db, user.ID, "all-tiers-dept")

	// User: 100000 (generous), Group: 50000, Department: 20000, System: 1000000.
	quotaSvc.Create("user", int64ToString(user.ID), 100000)
	quotaSvc.Create("group", int64ToString(g.ID), 50000)
	quotaSvc.Create("department", "all-tiers-dept", 20000)
	quotaSvc.Create("system", "", 1000000)

	// 5000 fits all four tiers.
	if err := quotaSvc.CheckUserQuota(user.ID, &tokenID, 5000); err != nil {
		t.Errorf("all-fits: %v", err)
	}
	// Use 15000 via the dept token so dept total = 15000.
	u, _ := uploadSvc.Create(user.ID, &tokenID, "d.bin", "d.bin", 15000, "application/octet-stream", "private", "/tmp/d.bin", nil)
	uploadSvc.MarkCompleted(u.ID, "md", "1")

	// User tier: 15000 + 4000 = 19000, well under 100000 → OK on user tier.
	// Group tier: 15000 + 4000 = 19000, well under 50000 → OK on group tier.
	// Department tier: 15000 + 4000 = 19000, still under 20000 → OK.
	if err := quotaSvc.CheckUserQuota(user.ID, &tokenID, 4000); err != nil {
		t.Errorf("under all four tiers: %v", err)
	}
	// Department tier: 15000 + 6000 = 21000 > 20000 → rejected (dept is the
	// tightest tier, so it wins).
	if err := quotaSvc.CheckUserQuota(user.ID, &tokenID, 6000); err != ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded from department tier, got %v", err)
	}
}

// --- calcUsage display ------------------------------------------------------

func TestQuotaGroupUsageDisplay(t *testing.T) {
	db := setupTestDB(t)
	fx := setupGroupFixture(t, db, 1200, 800)
	quotaSvc := NewQuotaService(db)

	q, _ := quotaSvc.Create("group", int64ToString(fx.groupID), 100000)
	got, _ := quotaSvc.GetByID(q.ID)
	if got.UsedBytes != 2000 {
		t.Errorf("group used_bytes = %d, want 2000", got.UsedBytes)
	}
}

func TestQuotaDepartmentUsageDisplay(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "dept-usage@example.com", "D", "U")
	tokenID := makeDeptToken(t, db, user.ID, "ops")
	uploadSvc := NewUploadService(db)
	quotaSvc := NewQuotaService(db)

	u1, _ := uploadSvc.Create(user.ID, &tokenID, "a.bin", "a.bin", 1500, "application/octet-stream", "private", "/tmp/a.bin", nil)
	uploadSvc.MarkCompleted(u1.ID, "ma", "1")
	u2, _ := uploadSvc.Create(user.ID, &tokenID, "b.bin", "b.bin", 2500, "application/octet-stream", "private", "/tmp/b.bin", nil)
	uploadSvc.MarkCompleted(u2.ID, "mb", "1")

	q, _ := quotaSvc.Create("department", "ops", 100000)
	got, _ := quotaSvc.GetByID(q.ID)
	if got.UsedBytes != 4000 {
		t.Errorf("dept used_bytes = %d, want 4000", got.UsedBytes)
	}
}
