package services

import "testing"

// TestPaymentReconcileHelpers covers the service-layer support for the
// pay-then-fail reconciliation path (V2-425 / V2-426): HasByUpload, the
// temp-preserving failure mark, and its inclusion in the active temp-path set so
// the GC never shreds paid-for data.
func TestPaymentReconcileHelpers(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	txnSvc := NewTransactionService(db)
	walletSvc := NewWalletService(db, "1111111111111111111111111111111111111111111111111111111111111111")

	user := createTestUser(t, userSvc, "recon@test.local", "R", "R")
	const tempPath = "/tmp/recon-test.bin"
	up, err := uploadSvc.Create(user.ID, nil, "f.bin", "f.bin", 10, "application/octet-stream", "private", tempPath, nil)
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}

	// No payment yet.
	if has, err := txnSvc.HasByUpload(up.ID); err != nil || has {
		t.Fatalf("HasByUpload before payment = %v (err %v), want false", has, err)
	}

	// Record a payment → HasByUpload true.
	wallet, err := walletSvc.Create("w", "0xabc", "deadbeefprivatekey")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if _, err := txnSvc.Record(wallet.ID, &up.ID, "upload", "100", "900", "0xhash"); err != nil {
		t.Fatalf("record txn: %v", err)
	}
	if has, err := txnSvc.HasByUpload(up.ID); err != nil || !has {
		t.Fatalf("HasByUpload after payment = %v (err %v), want true", has, err)
	}

	// MarkFailedPreserveTemp keeps temp_path + sets the recoverable detail.
	if err := uploadSvc.MarkFailedPreserveTemp(up.ID, "finalize failed", StatusDetailPaidUnfinalized); err != nil {
		t.Fatalf("MarkFailedPreserveTemp: %v", err)
	}
	got, err := uploadSvc.GetByID(up.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !got.TempPath.Valid || got.TempPath.String != tempPath {
		t.Errorf("temp_path = %v, want preserved (%q)", got.TempPath, tempPath)
	}
	if got.StatusDetail.String != StatusDetailPaidUnfinalized {
		t.Errorf("status_detail = %q, want %q", got.StatusDetail.String, StatusDetailPaidUnfinalized)
	}

	// The preserved temp path stays in the active set (GC won't shred it).
	paths, err := uploadSvc.ListActiveTempPaths()
	if err != nil {
		t.Fatalf("ListActiveTempPaths: %v", err)
	}
	found := false
	for _, p := range paths {
		if p == tempPath {
			found = true
		}
	}
	if !found {
		t.Error("ListActiveTempPaths should include a paid_unfinalized upload's temp path")
	}

	// Contrast: a plain MarkFailed (the abandon path) nulls temp_path.
	up2, err := uploadSvc.Create(user.ID, nil, "g.bin", "g.bin", 10, "application/octet-stream", "private", "/tmp/recon-test2.bin", nil)
	if err != nil {
		t.Fatalf("create upload 2: %v", err)
	}
	if err := uploadSvc.MarkFailed(up2.ID, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	got2, err := uploadSvc.GetByID(up2.ID)
	if err != nil {
		t.Fatalf("GetByID 2: %v", err)
	}
	if got2.TempPath.Valid {
		t.Error("MarkFailed should null temp_path (abandon path)")
	}
}
