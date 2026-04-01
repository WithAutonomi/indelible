package services

import (
	"strings"
	"testing"
)

func TestScimTokenCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	secret, token, err := svc.Create("CI/CD Token", admin.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(secret, "scim_") {
		t.Errorf("secret should have scim_ prefix, got %q", secret[:10])
	}
	// scim_ + 64 hex = 69
	if len(secret) != 69 {
		t.Errorf("secret length = %d, want 69", len(secret))
	}
	if token.Name != "CI/CD Token" {
		t.Errorf("Name = %q", token.Name)
	}
	if !token.IsActive {
		t.Error("new token should be active")
	}
	if token.CreatedBy != admin.ID {
		t.Errorf("CreatedBy = %d", token.CreatedBy)
	}
}

func TestScimTokenValidate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	secret, created, _ := svc.Create("Token1", admin.ID)

	token, err := svc.Validate(secret)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if token.ID != created.ID {
		t.Errorf("validated token ID = %d, want %d", token.ID, created.ID)
	}
}

func TestScimTokenValidate_WrongSecret(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	svc.Create("Token1", admin.ID)

	_, err := svc.Validate("scim_wrong_secret_value_here")
	if err != ErrScimTokenNotFound {
		t.Errorf("expected ErrScimTokenNotFound, got %v", err)
	}
}

func TestScimTokenRevoke(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	secret, token, _ := svc.Create("Token1", admin.ID)

	if err := svc.Revoke(token.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Token should no longer validate
	_, err := svc.Validate(secret)
	if err != ErrScimTokenNotFound {
		t.Errorf("revoked token should not validate, got %v", err)
	}

	// Token record should show revoked
	got, _ := svc.GetByID(token.ID)
	if got.IsActive {
		t.Error("revoked token should be inactive")
	}
	if !got.RevokedAt.Valid {
		t.Error("revoked_at should be set")
	}
}

func TestScimTokenRevoke_AlreadyRevoked(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	_, token, _ := svc.Create("Token1", admin.ID)
	svc.Revoke(token.ID)

	err := svc.Revoke(token.ID)
	if err != ErrScimTokenNotFound {
		t.Errorf("double revoke should fail, got %v", err)
	}
}

func TestScimTokenList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	svc.Create("Token1", admin.ID)
	_, tok2, _ := svc.Create("Token2", admin.ID)
	svc.Revoke(tok2.ID)

	tokens, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// List includes revoked tokens for audit
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens (including revoked), got %d", len(tokens))
	}
}

func TestScimTokenRecordUsage(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)
	userSvc := NewUserService(db)
	admin := createTestUser(t, userSvc, "admin@test.com", "Admin", "User")

	_, token, _ := svc.Create("Token1", admin.ID)

	// Initially no last_used_at
	if token.LastUsedAt.Valid {
		t.Error("last_used_at should be null initially")
	}

	svc.RecordUsage(token.ID)

	got, _ := svc.GetByID(token.ID)
	if !got.LastUsedAt.Valid {
		t.Error("last_used_at should be set after RecordUsage")
	}
}

func TestScimTokenGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewScimTokenService(db)

	_, err := svc.GetByID(999)
	if err != ErrScimTokenNotFound {
		t.Errorf("expected ErrScimTokenNotFound, got %v", err)
	}
}
