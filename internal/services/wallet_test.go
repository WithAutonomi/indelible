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

// --- Wallet crypto round-trip + tamper-detection (V2-281 item 3) -----------
//
// The crypto package has unit tests for raw Encrypt/Decrypt, but those don't
// prove WalletService persists + retrieves the ciphertext correctly. These
// tests round-trip via the service layer so a future change to DB column
// types, charset coercion (Postgres TEXT vs BYTEA), or the encoding helper
// would be caught here.

func TestWalletCreate_RoundTripPrivateKey(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	const pk = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	w, err := svc.Create("rt", "0xRoundTrip", pk)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	decrypted, err := svc.DecryptKey(w)
	if err != nil {
		t.Fatalf("DecryptKey: %v", err)
	}
	if decrypted != pk {
		t.Errorf("round trip mismatch: got %q, want %q", decrypted, pk)
	}
}

func TestWalletDecrypt_WrongKeyFails(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w, err := svc.Create("wk", "0xWrongKey", "private-bytes-here")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Same DB, different key — the existing ciphertext can't be decrypted.
	otherKey := "1111111111111111111111111111111111111111111111111111111111111111"
	other := NewWalletService(db, otherKey)
	stored, err := other.GetByID(w.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if _, err := other.DecryptKey(stored); err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestWalletDecrypt_TamperedCiphertextFailsAEAD(t *testing.T) {
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w, err := svc.Create("tamper", "0xTamper", "secret-private-key")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Flip the last hex character of the ciphertext — corrupts the GCM tag
	// so AEAD validation must reject. Pick a different hex char to guarantee
	// a real bit-flip.
	orig := w.EncryptedKey
	last := orig[len(orig)-1]
	swap := byte('0')
	if last == '0' {
		swap = '1'
	}
	tampered := orig[:len(orig)-1] + string(swap)

	// Write the tampered ciphertext back and re-fetch.
	if _, err := db.Exec(`UPDATE wallets SET encrypted_key = ? WHERE id = ?`, tampered, w.ID); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := svc.GetByID(w.ID)
	if _, err := svc.DecryptKey(got); err == nil {
		t.Error("AEAD should reject tampered ciphertext, but Decrypt returned no error")
	}
}

func TestWalletDecrypt_TruncatedCiphertextFails(t *testing.T) {
	// Truncated ciphertext should fail without panicking — the crypto layer
	// must report "ciphertext too short" cleanly.
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)
	w, _ := svc.Create("trunc", "0xT", "secret")
	if _, err := db.Exec(`UPDATE wallets SET encrypted_key = ? WHERE id = ?`, "deadbeef", w.ID); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := svc.GetByID(w.ID)
	_, err := svc.DecryptKey(got)
	if err == nil {
		t.Fatal("expected error on truncated ciphertext")
	}
	// Should be a real error string, not a panic — already caught above by
	// the nil check, but explicitly assert non-empty message for clarity.
	if err.Error() == "" {
		t.Error("error should carry a message")
	}
}

func TestWalletCreate_DifferentNoncePerEncryption(t *testing.T) {
	// Two encryptions of the same plaintext under the same key must produce
	// different ciphertexts thanks to the random nonce. Catches a regression
	// where someone "optimises" the nonce generator into determinism.
	db := setupTestDB(t)
	svc := NewWalletService(db, testEncKey)

	w1, _ := svc.Create("n1", "0xN1", "identical-secret")
	w2, _ := svc.Create("n2", "0xN2", "identical-secret")

	if w1.EncryptedKey == w2.EncryptedKey {
		t.Error("two encryptions of the same plaintext should differ (nonce reuse)")
	}
	// Both decrypt to the same plaintext.
	d1, _ := svc.DecryptKey(w1)
	d2, _ := svc.DecryptKey(w2)
	if d1 != "identical-secret" || d2 != "identical-secret" {
		t.Errorf("decrypted values mismatch: %q vs %q", d1, d2)
	}
}

func TestNewWalletService_BadKeyLengthFailsAtEncryptTime(t *testing.T) {
	// Service construction is lenient (no key validation), so the bad-key
	// surface is the next Encrypt call. Confirm that surface returns an
	// error rather than silently writing unencrypted bytes.
	db := setupTestDB(t)
	badKey := "deadbeef" // 4 bytes hex-decoded, not 32
	svc := NewWalletService(db, badKey)
	_, err := svc.Create("bad", "0xBad", "private-key")
	if err == nil {
		t.Error("Create with bad key length should fail")
	}
	if err != nil && !strings.Contains(err.Error(), "key") {
		t.Errorf("error should mention the key, got %q", err.Error())
	}
}
