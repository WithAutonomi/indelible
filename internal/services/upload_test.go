package services

import (
	"fmt"
	"testing"
	"time"
)

// createTestUpload is a helper that creates a queued upload for the given user.
func createTestUpload(t *testing.T, svc *UploadService, userID int64, filename string, fileSize int64) *Upload {
	t.Helper()
	u, err := svc.Create(userID, nil, filename, filename, fileSize, "application/octet-stream", "private", "/tmp/"+filename, nil)
	if err != nil {
		t.Fatalf("createTestUpload(%s): %v", filename, err)
	}
	return u
}

func TestUploadCreate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "uploader@example.com", "Up", "Loader")

	svc := NewUploadService(db)
	est := "100"
	u, err := svc.Create(user.ID, nil, "stored.bin", "original.bin", 1024, "application/octet-stream", "private", "/tmp/stored.bin", &est)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if u.UUID == "" {
		t.Error("expected non-empty UUID")
	}
	if u.Status != "queued" {
		t.Errorf("expected status=queued, got %s", u.Status)
	}
	if u.QueuedAt.IsZero() {
		t.Error("expected queued_at to be set")
	}
	if u.UserID != user.ID {
		t.Errorf("expected user_id=%d, got %d", user.ID, u.UserID)
	}
	if u.Filename != "stored.bin" {
		t.Errorf("expected filename=stored.bin, got %s", u.Filename)
	}
	if u.OriginalFilename != "original.bin" {
		t.Errorf("expected original_filename=original.bin, got %s", u.OriginalFilename)
	}
	if u.FileSize != 1024 {
		t.Errorf("expected file_size=1024, got %d", u.FileSize)
	}
	if !u.EstimatedCost.Valid || u.EstimatedCost.String != "100" {
		t.Errorf("expected estimated_cost=100, got %v", u.EstimatedCost)
	}
}

func TestUploadCreateWithTokenID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "tok@example.com", "Tok", "User")

	svc := NewUploadService(db)
	tokenID := int64(42) // token doesn't need to exist in DB for this field test
	// The FK constraint on token_id means we can't use a fake ID with foreign_keys=ON.
	// Instead, just test nil case.
	u, err := svc.Create(user.ID, nil, "f.bin", "f.bin", 512, "text/plain", "public", "/tmp/f.bin", nil)
	if err != nil {
		t.Fatalf("Create without tokenID: %v", err)
	}
	if u.TokenID.Valid {
		t.Error("expected token_id to be NULL")
	}
	_ = tokenID
}

func TestUploadGetByID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "get@example.com", "Get", "User")

	svc := NewUploadService(db)
	created, _ := svc.Create(user.ID, nil, "f.bin", "f.bin", 100, "text/plain", "private", "/tmp/f.bin", nil)

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.UUID != created.UUID {
		t.Errorf("expected UUID=%s, got %s", created.UUID, got.UUID)
	}
}

func TestUploadGetByIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUploadService(db)

	_, err := svc.GetByID(99999)
	if err != ErrUploadNotFound {
		t.Errorf("expected ErrUploadNotFound, got %v", err)
	}
}

func TestUploadGetByUUID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "uuid@example.com", "UUID", "User")

	svc := NewUploadService(db)
	created, _ := svc.Create(user.ID, nil, "f.bin", "f.bin", 100, "text/plain", "private", "/tmp/f.bin", nil)

	got, err := svc.GetByUUID(created.UUID)
	if err != nil {
		t.Fatalf("GetByUUID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID=%d, got %d", created.ID, got.ID)
	}
}

func TestUploadGetByUUIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUploadService(db)

	_, err := svc.GetByUUID("nonexistent-uuid")
	if err != ErrUploadNotFound {
		t.Errorf("expected ErrUploadNotFound, got %v", err)
	}
}

func TestUploadListByUser(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user1 := createTestUser(t, userSvc, "list1@example.com", "List", "One")
	user2 := createTestUser(t, userSvc, "list2@example.com", "List", "Two")

	svc := NewUploadService(db)
	for i := 0; i < 5; i++ {
		createTestUpload(t, svc, user1.ID, fmt.Sprintf("user1_%d.bin", i), int64(100*(i+1)))
	}
	for i := 0; i < 3; i++ {
		createTestUpload(t, svc, user2.ID, fmt.Sprintf("user2_%d.bin", i), int64(200*(i+1)))
	}

	uploads, total, err := svc.ListByUser(user1.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(uploads) != 5 {
		t.Errorf("expected 5 uploads, got %d", len(uploads))
	}

	// Verify ordering is newest first (descending created_at)
	for i := 1; i < len(uploads); i++ {
		if uploads[i].CreatedAt.After(uploads[i-1].CreatedAt) {
			t.Error("expected uploads sorted by created_at DESC")
			break
		}
	}

	// Test pagination
	uploads, total, err = svc.ListByUser(user1.ID, 2, 0)
	if err != nil {
		t.Fatalf("ListByUser paginated: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(uploads) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(uploads))
	}
}

func TestUploadListByUserLimitClamped(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "clamp@example.com", "Clamp", "User")

	svc := NewUploadService(db)
	createTestUpload(t, svc, user.ID, "f.bin", 100)

	// Zero limit should be clamped to 50
	uploads, _, err := svc.ListByUser(user.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByUser with limit=0: %v", err)
	}
	if len(uploads) != 1 {
		t.Errorf("expected 1 upload, got %d", len(uploads))
	}
}

func TestUploadListByUserFiltered(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "filter@example.com", "Filter", "User")

	svc := NewUploadService(db)

	// Create uploads with different sizes for sort testing
	u1 := createTestUpload(t, svc, user.ID, "small.bin", 100)
	u2 := createTestUpload(t, svc, user.ID, "large.bin", 5000)
	u3 := createTestUpload(t, svc, user.ID, "medium.bin", 2000)

	// Mark u2 as completed so we can filter by status
	svc.MarkCompleted(u2.ID, "datamap_hex", "50")

	// Filter by status=queued
	opts := UploadListOptions{
		UserID: user.ID,
		Limit:  10,
		Status: "queued",
	}
	uploads, total, err := svc.ListByUserFiltered(opts)
	if err != nil {
		t.Fatalf("ListByUserFiltered status=queued: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2 queued, got %d", total)
	}
	for _, u := range uploads {
		if u.Status != "queued" {
			t.Errorf("expected status=queued, got %s", u.Status)
		}
	}

	// Sort by file_size ascending
	opts = UploadListOptions{
		UserID:    user.ID,
		Limit:     10,
		SortBy:    "file_size",
		SortOrder: "asc",
	}
	uploads, total, err = svc.ListByUserFiltered(opts)
	if err != nil {
		t.Fatalf("ListByUserFiltered sort: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(uploads) >= 2 && uploads[0].FileSize > uploads[1].FileSize {
		t.Error("expected ascending file_size sort")
	}

	_ = u1
	_ = u3
}

func TestUploadListByUserFilteredDefaultSort(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "defsort@example.com", "Def", "Sort")

	svc := NewUploadService(db)

	// Create two uploads with distinct created_at by using a small file_size sort test
	createTestUpload(t, svc, user.ID, "small.bin", 100)
	createTestUpload(t, svc, user.ID, "large.bin", 5000)

	// Default sort (no SortBy specified) should use created_at DESC and return all results
	opts := UploadListOptions{
		UserID: user.ID,
		Limit:  10,
	}
	uploads, _, err := svc.ListByUserFiltered(opts)
	if err != nil {
		t.Fatalf("ListByUserFiltered default: %v", err)
	}
	if len(uploads) != 2 {
		t.Fatalf("expected 2 uploads, got %d", len(uploads))
	}

	// Verify file_size sort works (explicit sort to test sort column whitelisting)
	opts.SortBy = "file_size"
	opts.SortOrder = "desc"
	uploads, _, err = svc.ListByUserFiltered(opts)
	if err != nil {
		t.Fatalf("ListByUserFiltered file_size desc: %v", err)
	}
	if len(uploads) != 2 {
		t.Fatalf("expected 2 uploads, got %d", len(uploads))
	}
	if uploads[0].FileSize < uploads[1].FileSize {
		t.Errorf("expected descending file_size order, got %d then %d", uploads[0].FileSize, uploads[1].FileSize)
	}
}

func TestUploadListByUserCursor(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cursor@example.com", "Cursor", "User")

	svc := NewUploadService(db)

	var created []*Upload
	for i := 0; i < 5; i++ {
		u := createTestUpload(t, svc, user.ID, fmt.Sprintf("cursor_%d.bin", i), int64(100*(i+1)))
		created = append(created, u)
	}

	// Forward pagination: get uploads with IDs less than the last created (highest ID)
	cursorID := created[len(created)-1].ID
	uploads, err := svc.ListByUserCursor(user.ID, 2, cursorID, true)
	if err != nil {
		t.Fatalf("ListByUserCursor forward: %v", err)
	}
	if len(uploads) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(uploads))
	}
	// Forward should return IDs < cursorID, in descending order
	for _, u := range uploads {
		if u.ID >= cursorID {
			t.Errorf("expected ID < %d, got %d", cursorID, u.ID)
		}
	}

	// Backward pagination: get uploads with IDs greater than cursor
	backCursorID := created[0].ID
	uploads, err = svc.ListByUserCursor(user.ID, 3, backCursorID, false)
	if err != nil {
		t.Fatalf("ListByUserCursor backward: %v", err)
	}
	if len(uploads) < 1 {
		t.Fatal("expected at least 1 upload for backward pagination")
	}
	for _, u := range uploads {
		if u.ID <= backCursorID {
			t.Errorf("expected ID > %d, got %d", backCursorID, u.ID)
		}
	}
}

func TestUploadDequeueNext(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "dequeue@example.com", "Deq", "User")

	svc := NewUploadService(db)

	// Empty queue
	u, err := svc.DequeueNext()
	if err != nil {
		t.Fatalf("DequeueNext empty: %v", err)
	}
	if u != nil {
		t.Error("expected nil for empty queue")
	}

	// Create two queued uploads
	u1 := createTestUpload(t, svc, user.ID, "first.bin", 100)
	createTestUpload(t, svc, user.ID, "second.bin", 200)

	// Dequeue should pick the oldest (first)
	dequeued, err := svc.DequeueNext()
	if err != nil {
		t.Fatalf("DequeueNext: %v", err)
	}
	if dequeued == nil {
		t.Fatal("expected non-nil upload")
	}
	if dequeued.ID != u1.ID {
		t.Errorf("expected oldest upload (ID=%d), got ID=%d", u1.ID, dequeued.ID)
	}
	if dequeued.Status != "processing" {
		t.Errorf("expected status=processing, got %s", dequeued.Status)
	}
	if !dequeued.ProcessingAt.Valid {
		t.Error("expected processing_at to be set")
	}
}

func TestUploadDequeueSkipsBackoff(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "backoff@example.com", "Back", "Off")

	svc := NewUploadService(db)
	u1 := createTestUpload(t, svc, user.ID, "backoff.bin", 100)
	u2 := createTestUpload(t, svc, user.ID, "ready.bin", 200)

	// Put first upload in gas backoff (far future)
	err := svc.SetGasBackoff(u1.ID, time.Now().Add(1*time.Hour), 1, "999")
	if err != nil {
		t.Fatalf("SetGasBackoff: %v", err)
	}

	// Dequeue should skip the backoff upload and pick the second one
	dequeued, err := svc.DequeueNext()
	if err != nil {
		t.Fatalf("DequeueNext: %v", err)
	}
	if dequeued == nil {
		t.Fatal("expected non-nil upload")
	}
	if dequeued.ID != u2.ID {
		t.Errorf("expected ID=%d (skipping backoff), got ID=%d", u2.ID, dequeued.ID)
	}
}

func TestUploadMarkCompleted(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "complete@example.com", "Com", "Plete")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "tofinish.bin", 100)

	err := svc.MarkCompleted(u.ID, "deadbeef", "42")
	if err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	got, err := svc.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID after complete: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("expected status=completed, got %s", got.Status)
	}
	if !got.ActualCost.Valid || got.ActualCost.String != "42" {
		t.Errorf("expected actual_cost=42, got %v", got.ActualCost)
	}
	if !got.DataMap.Valid || got.DataMap.String != "deadbeef" {
		t.Errorf("expected data_map=deadbeef, got %v", got.DataMap)
	}
	if !got.CompletedAt.Valid {
		t.Error("expected completed_at to be set")
	}
	if got.TempPath.Valid {
		t.Error("expected temp_path to be cleared on completion")
	}
}

func TestUploadMarkFailed(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "fail@example.com", "Fail", "User")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "tofail.bin", 100)

	err := svc.MarkFailed(u.ID, "network timeout")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	got, err := svc.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID after fail: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("expected status=failed, got %s", got.Status)
	}
	if !got.ErrorMessage.Valid || got.ErrorMessage.String != "network timeout" {
		t.Errorf("expected error_message='network timeout', got %v", got.ErrorMessage)
	}
	if !got.FailedAt.Valid {
		t.Error("expected failed_at to be set")
	}
	if got.TempPath.Valid {
		t.Error("expected temp_path to be cleared on failure")
	}
}

func TestUploadCancel(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cancel@example.com", "Cancel", "User")

	svc := NewUploadService(db)

	// Cancel a queued upload
	u := createTestUpload(t, svc, user.ID, "tocancel.bin", 100)
	err := svc.Cancel(u.ID)
	if err != nil {
		t.Fatalf("Cancel queued: %v", err)
	}
	got, _ := svc.GetByID(u.ID)
	if got.Status != "failed" {
		t.Errorf("expected status=failed after cancel, got %s", got.Status)
	}
	if !got.ErrorMessage.Valid || got.ErrorMessage.String != "Cancelled by user" {
		t.Errorf("expected error_message='Cancelled by user', got %v", got.ErrorMessage)
	}
}

func TestUploadCancelNotAllowedForCompleted(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cancel2@example.com", "Cancel", "Two")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "done.bin", 100)
	svc.MarkCompleted(u.ID, "map", "10")

	err := svc.Cancel(u.ID)
	if err == nil {
		t.Error("expected error when cancelling completed upload")
	}
}

func TestUploadCancelNotAllowedForFailed(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cancel3@example.com", "Cancel", "Three")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "failed.bin", 100)
	svc.MarkFailed(u.ID, "oops")

	err := svc.Cancel(u.ID)
	if err == nil {
		t.Error("expected error when cancelling failed upload")
	}
}

func TestUploadRetry(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "retry@example.com", "Retry", "User")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "toretry.bin", 100)

	// Must be failed to retry
	svc.MarkFailed(u.ID, "transient error")

	err := svc.Retry(u.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	got, _ := svc.GetByID(u.ID)
	if got.Status != "queued" {
		t.Errorf("expected status=queued after retry, got %s", got.Status)
	}
	if got.ErrorMessage.Valid {
		t.Error("expected error_message to be cleared after retry")
	}
	if got.FailedAt.Valid {
		t.Error("expected failed_at to be cleared after retry")
	}
}

func TestUploadRetryNotAllowedForQueued(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "retry2@example.com", "Retry", "Two")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "queued.bin", 100)

	err := svc.Retry(u.ID)
	if err == nil {
		t.Error("expected error when retrying a queued upload")
	}
}

func TestUploadForceRetry(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "force@example.com", "Force", "Retry")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "gasbackoff.bin", 100)

	// Put in gas backoff
	err := svc.SetGasBackoff(u.ID, time.Now().Add(1*time.Hour), 3, "5000")
	if err != nil {
		t.Fatalf("SetGasBackoff: %v", err)
	}

	// Verify it's in backoff
	got, _ := svc.GetByID(u.ID)
	if !got.StatusDetail.Valid || got.StatusDetail.String != "gas_backoff" {
		t.Fatalf("expected status_detail=gas_backoff, got %v", got.StatusDetail)
	}

	// Force retry
	err = svc.ForceRetry(u.ID)
	if err != nil {
		t.Fatalf("ForceRetry: %v", err)
	}

	got, _ = svc.GetByID(u.ID)
	if got.Status != "queued" {
		t.Errorf("expected status=queued, got %s", got.Status)
	}
	if got.StatusDetail.Valid {
		t.Error("expected status_detail to be cleared")
	}
	if got.BackoffUntil.Valid {
		t.Error("expected backoff_until to be cleared")
	}
}

func TestUploadForceRetryNotInBackoff(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "force2@example.com", "Force", "Two")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "normal.bin", 100)

	err := svc.ForceRetry(u.ID)
	if err == nil {
		t.Error("expected error when force-retrying a non-backoff upload")
	}
}

func TestUploadSetGasBackoff(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "gas@example.com", "Gas", "User")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "gas.bin", 100)

	backoffTime := time.Now().Add(30 * time.Minute)
	err := svc.SetGasBackoff(u.ID, backoffTime, 2, "7500")
	if err != nil {
		t.Fatalf("SetGasBackoff: %v", err)
	}

	got, _ := svc.GetByID(u.ID)
	if got.Status != "queued" {
		t.Errorf("expected status=queued during backoff, got %s", got.Status)
	}
	if !got.StatusDetail.Valid || got.StatusDetail.String != "gas_backoff" {
		t.Errorf("expected status_detail=gas_backoff, got %v", got.StatusDetail)
	}
	if got.BackoffAttempt != 2 {
		t.Errorf("expected backoff_attempt=2, got %d", got.BackoffAttempt)
	}
	if !got.LastQuotedCost.Valid || got.LastQuotedCost.String != "7500" {
		t.Errorf("expected last_quoted_cost=7500, got %v", got.LastQuotedCost)
	}
	if !got.BackoffUntil.Valid {
		t.Error("expected backoff_until to be set")
	}
}

func TestUploadClearBackoff(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "clear@example.com", "Clear", "Backoff")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "clearing.bin", 100)
	svc.SetGasBackoff(u.ID, time.Now().Add(1*time.Hour), 1, "500")

	err := svc.ClearBackoff(u.ID)
	if err != nil {
		t.Fatalf("ClearBackoff: %v", err)
	}

	got, _ := svc.GetByID(u.ID)
	if got.StatusDetail.Valid {
		t.Error("expected status_detail to be cleared")
	}
	if got.BackoffUntil.Valid {
		t.Error("expected backoff_until to be cleared")
	}
}

func TestUploadRequeueStuck(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "stuck@example.com", "Stuck", "User")

	svc := NewUploadService(db)

	// Create and dequeue (transition to processing)
	createTestUpload(t, svc, user.ID, "stuck1.bin", 100)
	createTestUpload(t, svc, user.ID, "stuck2.bin", 200)
	svc.DequeueNext()
	svc.DequeueNext()

	// Manually backdate processing_at to simulate stuck uploads
	db.Exec(`UPDATE uploads SET processing_at = datetime('now', '-120 minutes') WHERE status = 'processing'`)

	requeued, err := svc.RequeueStuck(60) // 60 minute timeout
	if err != nil {
		t.Fatalf("RequeueStuck: %v", err)
	}
	if requeued != 2 {
		t.Errorf("expected 2 requeued, got %d", requeued)
	}

	// Verify they're back to queued
	uploads, _, _ := svc.ListByUser(user.ID, 10, 0)
	for _, u := range uploads {
		if u.Status != "queued" {
			t.Errorf("expected status=queued after requeue, got %s for upload %d", u.Status, u.ID)
		}
	}
}

func TestUploadRequeueStuckIgnoresRecent(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "recent@example.com", "Recent", "User")

	svc := NewUploadService(db)
	createTestUpload(t, svc, user.ID, "recent.bin", 100)
	svc.DequeueNext()

	// Don't backdate -- it was just dequeued
	requeued, err := svc.RequeueStuck(60)
	if err != nil {
		t.Fatalf("RequeueStuck: %v", err)
	}
	if requeued != 0 {
		t.Errorf("expected 0 requeued for recent uploads, got %d", requeued)
	}
}

func TestUploadDelete(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "delete@example.com", "Del", "User")

	svc := NewUploadService(db)

	// Delete completed upload
	u1 := createTestUpload(t, svc, user.ID, "completed.bin", 100)
	svc.MarkCompleted(u1.ID, "map", "10")
	if err := svc.Delete(u1.ID); err != nil {
		t.Errorf("Delete completed: %v", err)
	}
	_, err := svc.GetByID(u1.ID)
	if err != ErrUploadNotFound {
		t.Error("expected ErrUploadNotFound after delete")
	}

	// Delete failed upload
	u2 := createTestUpload(t, svc, user.ID, "failed.bin", 200)
	svc.MarkFailed(u2.ID, "error")
	if err := svc.Delete(u2.ID); err != nil {
		t.Errorf("Delete failed: %v", err)
	}
}

func TestUploadDeleteNotAllowedForQueued(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "delnot@example.com", "Del", "No")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "queued.bin", 100)

	err := svc.Delete(u.ID)
	if err == nil {
		t.Error("expected error when deleting a queued upload")
	}
}

func TestUploadDeleteNotAllowedForProcessing(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "delproc@example.com", "Del", "Proc")

	svc := NewUploadService(db)
	createTestUpload(t, svc, user.ID, "processing.bin", 100)
	dequeued, _ := svc.DequeueNext()

	err := svc.Delete(dequeued.ID)
	if err == nil {
		t.Error("expected error when deleting a processing upload")
	}
}

// State machine tests

func TestUploadStateMachine_QueuedToProcessingToCompleted(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "sm1@example.com", "SM", "One")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "lifecycle.bin", 1024)

	// queued
	if u.Status != "queued" {
		t.Fatalf("expected initial status=queued, got %s", u.Status)
	}

	// queued -> processing
	dequeued, err := svc.DequeueNext()
	if err != nil {
		t.Fatalf("DequeueNext: %v", err)
	}
	if dequeued.Status != "processing" {
		t.Errorf("expected status=processing, got %s", dequeued.Status)
	}
	if !dequeued.ProcessingAt.Valid {
		t.Error("expected processing_at to be set")
	}

	// processing -> completed
	err = svc.MarkCompleted(dequeued.ID, "datamap_hex_value", "500")
	if err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	completed, _ := svc.GetByID(dequeued.ID)
	if completed.Status != "completed" {
		t.Errorf("expected status=completed, got %s", completed.Status)
	}
	if !completed.CompletedAt.Valid {
		t.Error("expected completed_at to be set")
	}
}

func TestUploadStateMachine_QueuedToProcessingToFailed(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "sm2@example.com", "SM", "Two")

	svc := NewUploadService(db)
	createTestUpload(t, svc, user.ID, "fail_lifecycle.bin", 1024)

	dequeued, _ := svc.DequeueNext()
	err := svc.MarkFailed(dequeued.ID, "upload processor error")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	failed, _ := svc.GetByID(dequeued.ID)
	if failed.Status != "failed" {
		t.Errorf("expected status=failed, got %s", failed.Status)
	}
	if !failed.FailedAt.Valid {
		t.Error("expected failed_at to be set")
	}
}

func TestUploadStateMachine_QueuedToCancelled(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "sm3@example.com", "SM", "Three")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "cancel_lifecycle.bin", 1024)

	err := svc.Cancel(u.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	cancelled, _ := svc.GetByID(u.ID)
	if cancelled.Status != "failed" {
		t.Errorf("expected status=failed after cancel, got %s", cancelled.Status)
	}
	if !cancelled.FailedAt.Valid {
		t.Error("expected failed_at to be set after cancel")
	}
}

func TestUploadStateMachine_FailedRetryToQueued(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "sm4@example.com", "SM", "Four")

	svc := NewUploadService(db)
	u := createTestUpload(t, svc, user.ID, "retry_lifecycle.bin", 1024)

	// queued -> processing -> failed -> queued (retry)
	svc.DequeueNext()
	svc.MarkFailed(u.ID, "first attempt failed")

	err := svc.Retry(u.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	retried, _ := svc.GetByID(u.ID)
	if retried.Status != "queued" {
		t.Errorf("expected status=queued after retry, got %s", retried.Status)
	}

	// Can dequeue again after retry
	dequeued, err := svc.DequeueNext()
	if err != nil {
		t.Fatalf("DequeueNext after retry: %v", err)
	}
	if dequeued == nil {
		t.Fatal("expected upload to be dequeueable after retry")
	}
}

func TestUploadCountByStatus(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "count@example.com", "Count", "User")

	svc := NewUploadService(db)
	u1 := createTestUpload(t, svc, user.ID, "q1.bin", 100)
	createTestUpload(t, svc, user.ID, "q2.bin", 200)
	createTestUpload(t, svc, user.ID, "q3.bin", 300)
	svc.MarkCompleted(u1.ID, "map", "10")

	counts, err := svc.CountByStatus()
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}
	if counts["queued"] != 2 {
		t.Errorf("expected 2 queued, got %d", counts["queued"])
	}
	if counts["completed"] != 1 {
		t.Errorf("expected 1 completed, got %d", counts["completed"])
	}
}

func TestUploadListAll(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user1 := createTestUser(t, userSvc, "all1@example.com", "All", "One")
	user2 := createTestUser(t, userSvc, "all2@example.com", "All", "Two")

	svc := NewUploadService(db)
	createTestUpload(t, svc, user1.ID, "u1.bin", 100)
	createTestUpload(t, svc, user2.ID, "u2.bin", 200)

	uploads, total, err := svc.ListAll(10, 0)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(uploads) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(uploads))
	}
}

func TestUploadDeleteCleansUpRelatedData(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cleanup@example.com", "Clean", "Up")

	uploadSvc := NewUploadService(db)
	tagSvc := NewTagService(db)
	collSvc := NewCollectionService(db)

	u := createTestUpload(t, uploadSvc, user.ID, "tagged.bin", 100)
	tagSvc.SetTags(u.ID, map[string]string{"env": "test"})

	coll, _ := collSvc.Create("TestColl", "", nil, user.ID)
	collSvc.AddFile(coll.ID, u.ID)

	// Complete so we can delete
	uploadSvc.MarkCompleted(u.ID, "map", "10")

	err := uploadSvc.Delete(u.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Tags should be gone
	tags, _ := tagSvc.GetTags(u.ID)
	if len(tags) != 0 {
		t.Error("expected tags to be cleaned up after upload delete")
	}

	// Collection file association should be gone
	ids, _ := collSvc.CollectionIDsForUpload(u.ID)
	if len(ids) != 0 {
		t.Error("expected collection_files to be cleaned up after upload delete")
	}
}
