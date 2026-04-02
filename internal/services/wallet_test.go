package services

import (
	"strings"
	"testing"
)

const testEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestWalletCreate_FirstIsDefault(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w, err := svc.Create("w1", "0xABC", "privkey1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !w.IsDefault {
		t.Error("first wallet should be default")
	}
	if w.Name != "w1" || w.Address != "0xABC" {
		t.Errorf("got name=%q addr=%q", w.Name, w.Address)
	}
}

func TestWalletCreate_SecondNotDefault(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	svc.Create("w1", "0xAAA", "key1")
	w2, err := svc.Create("w2", "0xBBB", "key2")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if w2.IsDefault {
		t.Error("second wallet should not be default")
	}
}

func TestWalletCreate_KeyEncrypted(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w, err := svc.Create("w1", "0xABC", "my-secret-key")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if w.EncryptedKey == "my-secret-key" {
		t.Error("key should be encrypted, not stored in plaintext")
	}
	if w.EncryptedKey == "" {
		t.Error("encrypted key should not be empty")
	}
}

func TestWalletGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	created, _ := svc.Create("w1", "0xABC", "key1")

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID || got.Name != "w1" {
		t.Errorf("got %+v", got)
	}
}

func TestWalletGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	_, err := svc.GetByID(999)
	if err != ErrWalletNotFound {
		t.Errorf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestWalletGetDefault(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	svc.Create("w1", "0xAAA", "key1")
	svc.Create("w2", "0xBBB", "key2")

	def, err := svc.GetDefault()
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if def.Name != "w1" {
		t.Errorf("expected default=w1, got %q", def.Name)
	}
}

func TestWalletGetDefault_None(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	_, err := svc.GetDefault()
	if err != ErrNoDefaultWallet {
		t.Errorf("expected ErrNoDefaultWallet, got %v", err)
	}
}

func TestWalletList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	svc.Create("w1", "0xA", "k1")
	svc.Create("w2", "0xB", "k2")

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 wallets, got %d", len(list))
	}
}

func TestWalletSetDefault(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w1, _ := svc.Create("w1", "0xA", "k1")
	w2, _ := svc.Create("w2", "0xB", "k2")

	if err := svc.SetDefault(w2.ID); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	// w1 should no longer be default
	got1, _ := svc.GetByID(w1.ID)
	if got1.IsDefault {
		t.Error("w1 should no longer be default")
	}

	// w2 should be default
	got2, _ := svc.GetByID(w2.ID)
	if !got2.IsDefault {
		t.Error("w2 should be default")
	}
}

func TestWalletSetDefault_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	if err := svc.SetDefault(999); err != ErrWalletNotFound {
		t.Errorf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestWalletDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	svc.Create("w1", "0xA", "k1")
	w2, _ := svc.Create("w2", "0xB", "k2")

	if err := svc.Delete(w2.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := svc.GetByID(w2.ID)
	if err != ErrWalletNotFound {
		t.Errorf("expected ErrWalletNotFound after delete, got %v", err)
	}
}

func TestWalletDelete_CannotDeleteDefault(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w1, _ := svc.Create("w1", "0xA", "k1")

	err := svc.Delete(w1.ID)
	if err == nil {
		t.Error("should not be able to delete default wallet")
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("error should mention default: %v", err)
	}
}

func TestWalletDecryptKey(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w, _ := svc.Create("w1", "0xA", "super-secret-key")

	plain, err := svc.DecryptKey(w)
	if err != nil {
		t.Fatalf("DecryptKey: %v", err)
	}
	if plain != "super-secret-key" {
		t.Errorf("expected 'super-secret-key', got %q", plain)
	}
}

func TestWalletUpdateBalance(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w, _ := svc.Create("w1", "0xA", "k1")
	if w.PaymentBalance != "0" || w.GasBalance != "0" {
		t.Errorf("initial balances should be 0, got payment=%q gas=%q", w.PaymentBalance, w.GasBalance)
	}

	if err := svc.UpdateBalance(w.ID, "1000000", "500000"); err != nil {
		t.Fatalf("UpdateBalance: %v", err)
	}

	got, _ := svc.GetByID(w.ID)
	if got.PaymentBalance != "1000000" {
		t.Errorf("expected payment=1000000, got %q", got.PaymentBalance)
	}
	if got.GasBalance != "500000" {
		t.Errorf("expected gas=500000, got %q", got.GasBalance)
	}
}
