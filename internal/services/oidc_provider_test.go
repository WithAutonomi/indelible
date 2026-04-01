package services

import (
	"testing"
)

func TestOIDCProviderCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	p, err := svc.Create("google", "Google", "https://accounts.google.com", "client123", "secret456", "openid,email")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Name != "google" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.DisplayName != "Google" {
		t.Errorf("DisplayName = %q", p.DisplayName)
	}
	if p.IssuerURL != "https://accounts.google.com" {
		t.Errorf("IssuerURL = %q", p.IssuerURL)
	}
	if p.ClientID != "client123" {
		t.Errorf("ClientID = %q", p.ClientID)
	}
	if p.EncryptedSecret == "secret456" {
		t.Error("client secret should be encrypted, not stored in plaintext")
	}
	if p.EncryptedSecret == "" {
		t.Error("encrypted secret should not be empty")
	}
	if p.Scopes != "openid,email" {
		t.Errorf("Scopes = %q", p.Scopes)
	}
	if !p.IsEnabled {
		t.Error("new provider should be enabled by default")
	}
}

func TestOIDCProviderCreate_DefaultScopes(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	p, _ := svc.Create("okta", "Okta", "https://okta.com", "c1", "s1", "")
	if p.Scopes != "openid,email,profile" {
		t.Errorf("default Scopes = %q, want 'openid,email,profile'", p.Scopes)
	}
}

func TestOIDCProviderGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	created, _ := svc.Create("google", "Google", "https://accounts.google.com", "c1", "s1", "")

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "google" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestOIDCProviderGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	_, err := svc.GetByID(999)
	if err != ErrOIDCProviderNotFound {
		t.Errorf("expected ErrOIDCProviderNotFound, got %v", err)
	}
}

func TestOIDCProviderList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	svc.Create("google", "Google", "https://accounts.google.com", "c1", "s1", "")
	svc.Create("okta", "Okta", "https://okta.com", "c2", "s2", "")

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 providers, got %d", len(list))
	}
	// Ordered by name
	if list[0].Name != "google" {
		t.Errorf("first = %q, want google", list[0].Name)
	}
}

func TestOIDCProviderUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	p, _ := svc.Create("google", "Google", "https://accounts.google.com", "c1", "s1", "openid")

	updated, err := svc.Update(p.ID, "google-workspace", "Google Workspace", "https://accounts.google.com", "c2", "new_secret", "openid,email,profile", false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "google-workspace" {
		t.Errorf("Name = %q", updated.Name)
	}
	if updated.DisplayName != "Google Workspace" {
		t.Errorf("DisplayName = %q", updated.DisplayName)
	}
	if updated.ClientID != "c2" {
		t.Errorf("ClientID = %q", updated.ClientID)
	}
	if updated.IsEnabled {
		t.Error("should be disabled after update")
	}
	// Secret should have been re-encrypted (different from original)
	if updated.EncryptedSecret == p.EncryptedSecret {
		t.Error("encrypted secret should differ after update with new secret")
	}
}

func TestOIDCProviderUpdate_KeepSecret(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	p, _ := svc.Create("google", "Google", "https://accounts.google.com", "c1", "s1", "openid")
	origSecret := p.EncryptedSecret

	// Update without changing secret (empty clientSecret)
	updated, err := svc.Update(p.ID, "google-v2", "Google v2", "https://accounts.google.com", "c1", "", "openid", true)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.EncryptedSecret != origSecret {
		t.Error("encrypted secret should be unchanged when empty secret passed")
	}
	if updated.Name != "google-v2" {
		t.Errorf("Name = %q", updated.Name)
	}
}

func TestOIDCProviderDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	p, _ := svc.Create("google", "Google", "https://accounts.google.com", "c1", "s1", "")

	if err := svc.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := svc.GetByID(p.ID)
	if err != ErrOIDCProviderNotFound {
		t.Errorf("expected ErrOIDCProviderNotFound after delete, got %v", err)
	}
}

func TestOIDCProviderDelete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	err := svc.Delete(999)
	if err != ErrOIDCProviderNotFound {
		t.Errorf("expected ErrOIDCProviderNotFound, got %v", err)
	}
}

func TestOIDCProviderDelete_CleansIdentities(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	p, _ := svc.Create("google", "Google", "https://accounts.google.com", "c1", "s1", "")

	// Insert an OIDC identity
	_, err := db.Exec(`INSERT INTO oidc_identities (user_id, provider_id, subject) VALUES (?, ?, ?)`,
		user.ID, p.ID, "google-sub-123")
	if err != nil {
		t.Fatalf("insert identity: %v", err)
	}

	// Delete should clean up identities
	if err := svc.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM oidc_identities WHERE provider_id = ?`, p.ID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 identities after delete, got %d", count)
	}
}

func TestOIDCProviderSecretEncryptDecrypt(t *testing.T) {
	db := setupTestDB(t)
	svc := NewOIDCProviderService(db, testEncKey)

	secret := "my-oauth-client-secret-value"
	p, _ := svc.Create("test", "Test", "https://test.com", "c1", secret, "")

	// The encrypted secret should not be the plaintext
	if p.EncryptedSecret == secret {
		t.Error("secret stored as plaintext")
	}

	// Decrypt using the crypto package directly to verify roundtrip
	decrypted, err := decryptOIDCSecret(testEncKey, p.EncryptedSecret)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != secret {
		t.Errorf("decrypted = %q, want %q", decrypted, secret)
	}
}

// decryptOIDCSecret is a test helper that decrypts an OIDC client secret.
func decryptOIDCSecret(key, ciphertext string) (string, error) {
	return (&WalletService{encryptionKey: key}).DecryptKey(&Wallet{EncryptedKey: ciphertext})
}
