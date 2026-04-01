package services

import (
	"testing"
	"time"
)

func TestEmailVerificationCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewEmailVerificationService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, err := svc.Create(user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}
}

func TestEmailVerificationValidate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewEmailVerificationService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	// User starts unverified
	if user.EmailVerified {
		t.Error("user should start unverified")
	}

	token, _ := svc.Create(user.ID)

	userID, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if userID != user.ID {
		t.Errorf("userID = %d, want %d", userID, user.ID)
	}

	// User should now be verified
	updated, _ := userSvc.GetByID(user.ID)
	if !updated.EmailVerified {
		t.Error("user should be email_verified after validation")
	}
}

func TestEmailVerificationValidate_UsedToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewEmailVerificationService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, _ := svc.Create(user.ID)

	// First use
	svc.Validate(token)

	// Second use fails
	_, err := svc.Validate(token)
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for used token, got %v", err)
	}
}

func TestEmailVerificationValidate_ExpiredToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewEmailVerificationService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	token, _ := svc.Create(user.ID)

	// Expire the token
	_, err := db.Exec(
		`UPDATE email_verification_tokens SET expires_at = ? WHERE token = ?`,
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

func TestEmailVerificationValidate_InvalidToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewEmailVerificationService(db)

	_, err := svc.Validate("bogus_token")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestEmailVerificationCreate_InvalidatesPrevious(t *testing.T) {
	db := setupTestDB(t)
	svc := NewEmailVerificationService(db)
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
	_, err = svc.Validate(token2)
	if err != nil {
		t.Fatalf("new token Validate: %v", err)
	}
}
