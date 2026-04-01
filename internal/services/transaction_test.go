package services

import (
	"testing"
)

func TestTransactionRecord(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	txSvc := NewTransactionService(db)

	w, _ := walletSvc.Create("w1", "0xA", "key1")

	tx, err := txSvc.Record(w.ID, nil, "upload", "5000", "95000", "")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if tx.WalletID != w.ID {
		t.Errorf("WalletID = %d", tx.WalletID)
	}
	if tx.TxType != "upload" {
		t.Errorf("TxType = %q", tx.TxType)
	}
	if tx.Amount != "5000" {
		t.Errorf("Amount = %q", tx.Amount)
	}
	if tx.BalanceAfter != "95000" {
		t.Errorf("BalanceAfter = %q", tx.BalanceAfter)
	}
	if tx.UploadID.Valid {
		t.Error("UploadID should be null when nil passed")
	}
	if tx.TxHash.Valid {
		t.Error("TxHash should be null when empty")
	}
}

func TestTransactionRecord_WithUploadAndHash(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	txSvc := NewTransactionService(db)

	w, _ := walletSvc.Create("w1", "0xA", "key1")
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")
	upload, _ := uploadSvc.Create(user.ID, nil, "f.txt", "f.txt", 1024, "text/plain", "public", "/tmp/f.txt", nil)

	uploadID := upload.ID
	tx, err := txSvc.Record(w.ID, &uploadID, "upload", "1000", "99000", "0xABC123")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !tx.UploadID.Valid || tx.UploadID.Int64 != uploadID {
		t.Errorf("UploadID = %v", tx.UploadID)
	}
	if !tx.TxHash.Valid || tx.TxHash.String != "0xABC123" {
		t.Errorf("TxHash = %v", tx.TxHash)
	}
}

func TestTransactionGetByID(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	txSvc := NewTransactionService(db)

	w, _ := walletSvc.Create("w1", "0xA", "key1")
	recorded, _ := txSvc.Record(w.ID, nil, "upload", "500", "99500", "")

	got, err := txSvc.GetByID(recorded.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Amount != "500" {
		t.Errorf("Amount = %q", got.Amount)
	}
}

func TestTransactionListByWallet(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	txSvc := NewTransactionService(db)

	w1, _ := walletSvc.Create("w1", "0xA", "key1")
	w2, _ := walletSvc.Create("w2", "0xB", "key2")

	txSvc.Record(w1.ID, nil, "upload", "100", "900", "")
	txSvc.Record(w1.ID, nil, "upload", "200", "700", "")
	txSvc.Record(w2.ID, nil, "upload", "300", "700", "")

	txns, total, err := txSvc.ListByWallet(w1.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListByWallet: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(txns) != 2 {
		t.Errorf("entries = %d, want 2", len(txns))
	}
	// Verify both amounts are present (ordering within same second is non-deterministic)
	amounts := map[string]bool{}
	for _, tx := range txns {
		amounts[tx.Amount] = true
	}
	if !amounts["100"] || !amounts["200"] {
		t.Errorf("expected amounts 100 and 200, got %v", amounts)
	}
}

func TestTransactionListByWallet_Pagination(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	txSvc := NewTransactionService(db)

	w, _ := walletSvc.Create("w1", "0xA", "key1")

	for i := 0; i < 5; i++ {
		txSvc.Record(w.ID, nil, "upload", "100", "99900", "")
	}

	txns, total, err := txSvc.ListByWallet(w.ID, 2, 0)
	if err != nil {
		t.Fatalf("ListByWallet: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(txns) != 2 {
		t.Errorf("page size = %d, want 2", len(txns))
	}

	// Page 2
	txns2, _, _ := txSvc.ListByWallet(w.ID, 2, 2)
	if len(txns2) != 2 {
		t.Errorf("page 2 size = %d, want 2", len(txns2))
	}
}

func TestTransactionListByWallet_DefaultLimit(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	txSvc := NewTransactionService(db)

	w, _ := walletSvc.Create("w1", "0xA", "key1")

	// limit=0 should default to 50
	_, total, err := txSvc.ListByWallet(w.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByWallet: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d", total)
	}
}

func TestTransactionRefundType(t *testing.T) {
	db := setupTestDB(t)
	walletSvc := NewWalletService(db, testEncKey)
	txSvc := NewTransactionService(db)

	w, _ := walletSvc.Create("w1", "0xA", "key1")

	tx, err := txSvc.Record(w.ID, nil, "refund", "500", "100500", "")
	if err != nil {
		t.Fatalf("Record refund: %v", err)
	}
	if tx.TxType != "refund" {
		t.Errorf("TxType = %q, want refund", tx.TxType)
	}
}
