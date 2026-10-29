package services

import (
	"testing"
	"time"
)

// backdateProcessing makes every processing upload look idle for `ago`.
func backdateProcessing(t *testing.T, svc *UploadService, ago time.Duration) {
	t.Helper()
	ts := time.Now().UTC().Add(-ago).Format("2006-01-02 15:04:05")
	if _, err := svc.db.Exec(`UPDATE uploads SET processing_at = ? WHERE status = 'processing'`, ts); err != nil {
		t.Fatalf("backdate processing_at: %v", err)
	}
}

// RequeueStuckExcept must leave the excluded (in-flight) uploads in
// processing however old their heartbeat, and requeue the rest (V2-1773).
func TestRequeueStuckExceptSkipsExcluded(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "except@example.com", "Ex", "Cept")
	svc := NewUploadService(db)

	live := createTestUpload(t, svc, user.ID, "live.bin", 100)
	orphan := createTestUpload(t, svc, user.ID, "orphan.bin", 100)
	svc.DequeueNext()
	svc.DequeueNext()
	backdateProcessing(t, svc, 3*time.Hour)

	requeued, err := svc.RequeueStuckExcept(time.Hour, []int64{live.ID})
	if err != nil {
		t.Fatalf("RequeueStuckExcept: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued %d, want 1 (the orphan only)", requeued)
	}
	if got, _ := svc.GetByID(live.ID); got.Status != "processing" {
		t.Errorf("in-flight upload status = %q, want processing", got.Status)
	}
	if got, _ := svc.GetByID(orphan.ID); got.Status != "queued" {
		t.Errorf("orphan upload status = %q, want queued", got.Status)
	}
}

// With no exclusions, RequeueStuckExcept behaves like RequeueStuck.
func TestRequeueStuckExceptNoExclusions(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "noexcl@example.com", "No", "Excl")
	svc := NewUploadService(db)

	createTestUpload(t, svc, user.ID, "a.bin", 100)
	createTestUpload(t, svc, user.ID, "b.bin", 100)
	svc.DequeueNext()
	svc.DequeueNext()
	backdateProcessing(t, svc, 3*time.Hour)

	requeued, err := svc.RequeueStuckExcept(time.Hour, nil)
	if err != nil {
		t.Fatalf("RequeueStuckExcept: %v", err)
	}
	if requeued != 2 {
		t.Errorf("requeued %d, want 2", requeued)
	}
}

// TouchProcessing is the progress heartbeat: a touched row is no longer
// stuck. It never resurrects a row that has left processing.
func TestTouchProcessing(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "touch@example.com", "To", "Uch")
	svc := NewUploadService(db)

	u := createTestUpload(t, svc, user.ID, "touch.bin", 100)
	svc.DequeueNext()
	backdateProcessing(t, svc, 3*time.Hour)

	if err := svc.TouchProcessing(u.ID); err != nil {
		t.Fatalf("TouchProcessing: %v", err)
	}
	requeued, err := svc.RequeueStuckExcept(time.Hour, nil)
	if err != nil {
		t.Fatalf("RequeueStuckExcept: %v", err)
	}
	if requeued != 0 {
		t.Errorf("requeued %d after a fresh heartbeat, want 0", requeued)
	}

	// A completed row is not touched back to life.
	if err := svc.MarkCompleted(u.ID, "aabb", "0"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if err := svc.TouchProcessing(u.ID); err != nil {
		t.Fatalf("TouchProcessing (completed): %v", err)
	}
	got, _ := svc.GetByID(u.ID)
	if got.Status != "completed" {
		t.Errorf("status = %q after touching a completed row, want completed", got.Status)
	}
}

// HasPaymentSinceQueued: a payment recorded after the upload was queued
// blocks an automatic re-pay; an operator Retry (which moves queued_at
// forward) authorizes a new one (V2-1773).
func TestHasPaymentSinceQueued(t *testing.T) {
	db := setupTestDB(t)
	user := createTestUser(t, NewUserService(db), "repay@example.com", "Re", "Pay")
	uploadSvc := NewUploadService(db)
	txnSvc := NewTransactionService(db)
	walletSvc := NewWalletService(db, mustKR(t, "1111111111111111111111111111111111111111111111111111111111111111"))
	wallet, err := walletSvc.Create("w", "0xabc", "deadbeefprivatekey")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	u := createTestUpload(t, uploadSvc, user.ID, "repay.bin", 100)
	if paid, err := txnSvc.HasPaymentSinceQueued(u.ID); err != nil || paid {
		t.Fatalf("before any payment = %v (err %v), want false", paid, err)
	}

	// A payment during this run: an automatic re-run must not pay again.
	if _, err := txnSvc.Record(wallet.ID, &u.ID, "upload", "100", "900", "0xhash"); err != nil {
		t.Fatalf("record txn: %v", err)
	}
	if paid, err := txnSvc.HasPaymentSinceQueued(u.ID); err != nil || !paid {
		t.Fatalf("after a payment = %v (err %v), want true", paid, err)
	}

	// A sweep/restart requeue leaves queued_at alone: still refused.
	uploadSvc.DequeueNext()
	backdateProcessing(t, uploadSvc, 3*time.Hour)
	if _, err := uploadSvc.RequeueStuck(60); err != nil {
		t.Fatalf("RequeueStuck: %v", err)
	}
	if paid, err := txnSvc.HasPaymentSinceQueued(u.ID); err != nil || !paid {
		t.Fatalf("after a sweep requeue = %v (err %v), want true", paid, err)
	}

	// The payment predates an operator Retry: a new payment is authorized.
	// (Backdated so the check doesn't depend on the clock ticking over.)
	if _, err := db.Exec(`UPDATE transactions SET created_at = '2000-01-01 00:00:00' WHERE upload_id = ?`, u.ID); err != nil {
		t.Fatalf("backdate txn: %v", err)
	}
	if err := uploadSvc.MarkFailed(u.ID, "finalize failed"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := uploadSvc.Retry(u.ID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if paid, err := txnSvc.HasPaymentSinceQueued(u.ID); err != nil || paid {
		t.Fatalf("after an operator Retry = %v (err %v), want false", paid, err)
	}
}
