package services

import (
	"testing"
	"time"
)

func TestResetTokenCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewResetTokenService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, err := svc.Create(user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
	// Token is 32 bytes hex = 64 chars
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}
}

func TestResetTokenValidate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewResetTokenService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, _ := svc.Create(user.ID)

	userID, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if userID != user.ID {
		t.Errorf("userID = %d, want %d", userID, user.ID)
	}
}

func TestResetTokenValidate_UsedToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewResetTokenService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, _ := svc.Create(user.ID)

	// First use succeeds
	_, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("first Validate: %v", err)
	}

	// Second use fails (token marked as used)
	_, err = svc.Validate(token)
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for used token, got %v", err)
	}
}

func TestResetTokenValidate_InvalidToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewResetTokenService(db)

	_, err := svc.Validate("nonexistent_token")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestResetTokenValidate_ExpiredToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewResetTokenService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, _ := svc.Create(user.ID)

	// Manually expire the token by setting expires_at to the past
	_, err := db.Exec(
		`UPDATE password_reset_tokens SET expires_at = ? WHERE token = ?`,
		time.Now().Add(-1*time.Hour), token,
	)
	if err != nil {
		t.Fatalf("expire token: %v", err)
	}

	_, err = svc.Validate(token)
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for expired token, got %v", err)
	}
}

func TestResetTokenCreate_InvalidatesPrevious(t *testing.T) {
	db := setupTestDB(t)
	svc := NewResetTokenService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token1, _ := svc.Create(user.ID)
	token2, _ := svc.Create(user.ID)

	// First token should be invalidated
	_, err := svc.Validate(token1)
	if err != ErrInvalidCredentials {
		t.Errorf("old token should be invalidated, got %v", err)
	}

	// Second token should work
	userID, err := svc.Validate(token2)
	if err != nil {
		t.Fatalf("new token Validate: %v", err)
	}
	if userID != user.ID {
		t.Errorf("userID = %d", userID)
	}
}
