package services

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/crypto"
)

const (
	rotOldKey = "1111111111111111111111111111111111111111111111111111111111111111"
	rotNewKey = "2222222222222222222222222222222222222222222222222222222222222222"
)

func TestRotateEncryptionKey_WalletAndOIDC(t *testing.T) {
	db := setupTestDB(t)

	oldWalletSvc := NewWalletService(db, rotOldKey)
	w, err := oldWalletSvc.Create("primary", "0xabc", "wallet-private-key")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	oldOIDC := NewOIDCProviderService(db, rotOldKey)
	p, err := oldOIDC.Create("okta", "Okta", "https://issuer.example.com", "client-id", "oidc-client-secret", "")
	if err != nil {
		t.Fatalf("create oidc provider: %v", err)
	}

	// A legacy (un-tagged) wallet row, as written before key-id envelopes existed.
	legacyCT, err := crypto.Encrypt(rotOldKey, "legacy-private-key")
	if err != nil {
		t.Fatalf("legacy encrypt: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO wallets (name, address, encrypted_key, is_default) VALUES (?, ?, ?, ?)`,
		"legacy", "0xdef", legacyCT, false,
	); err != nil {
		t.Fatalf("insert legacy wallet: %v", err)
	}

	// Rotate old -> new.
	res, err := RotateEncryptionKey(db, rotOldKey, rotNewKey)
	if err != nil {
		t.Fatalf("RotateEncryptionKey: %v", err)
	}
	if res.Wallets != 2 {
		t.Errorf("rotated wallets = %d, want 2", res.Wallets)
	}
	if res.OIDCProviders != 1 {
		t.Errorf("rotated providers = %d, want 1", res.OIDCProviders)
	}

	// Everything decrypts under the NEW key with the original plaintext.
	newWalletSvc := NewWalletService(db, rotNewKey)
	gotW, _ := newWalletSvc.GetByID(w.ID)
	if pk, err := newWalletSvc.DecryptKey(gotW); err != nil || pk != "wallet-private-key" {
		t.Errorf("wallet decrypt under new key: got %q err %v", pk, err)
	}
	newKR, _ := crypto.NewKeyring(rotNewKey)
	gotP, _ := NewOIDCProviderService(db, rotNewKey).GetByID(p.ID)
	if sec, err := newKR.Decrypt(gotP.EncryptedSecret); err != nil || sec != "oidc-client-secret" {
		t.Errorf("oidc secret decrypt under new key: got %q err %v", sec, err)
	}

	// The legacy row also decrypts under the new key now (re-encrypted).
	all, _ := newWalletSvc.List()
	var foundLegacy bool
	for _, lw := range all {
		if lw.Name == "legacy" {
			foundLegacy = true
			if pk, err := newWalletSvc.DecryptKey(lw); err != nil || pk != "legacy-private-key" {
				t.Errorf("legacy wallet decrypt under new key: got %q err %v", pk, err)
			}
		}
	}
	if !foundLegacy {
		t.Error("legacy wallet not found after rotation")
	}

	// The OLD key can no longer decrypt the rotated rows (proves re-encryption).
	if _, err := oldWalletSvc.DecryptKey(gotW); err == nil {
		t.Error("expected old key to fail decrypting a rotated wallet")
	}
}

func TestRotateEncryptionKey_IdempotentReRun(t *testing.T) {
	db := setupTestDB(t)
	oldWalletSvc := NewWalletService(db, rotOldKey)
	w, err := oldWalletSvc.Create("w", "0x1", "pk")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	if _, err := RotateEncryptionKey(db, rotOldKey, rotNewKey); err != nil {
		t.Fatalf("first rotate: %v", err)
	}
	// Re-running the same rotation must be safe (rows already new-tagged; the
	// decrypt keyring still holds the new key).
	if _, err := RotateEncryptionKey(db, rotOldKey, rotNewKey); err != nil {
		t.Fatalf("re-run rotate: %v", err)
	}

	newWalletSvc := NewWalletService(db, rotNewKey)
	gotW, _ := newWalletSvc.GetByID(w.ID)
	if pk, err := newWalletSvc.DecryptKey(gotW); err != nil || pk != "pk" {
		t.Errorf("decrypt after re-run: got %q err %v", pk, err)
	}
}
