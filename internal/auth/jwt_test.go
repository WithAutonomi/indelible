package auth

import (
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	secret := "test-secret-key-1234567890"
	userID := int64(42)
	email := "test@example.com"

	token, err := GenerateToken(secret, userID, email, 24)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := ValidateToken(secret, token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID = %d, want %d", claims.UserID, userID)
	}
	if claims.Email != email {
		t.Errorf("Email = %s, want %s", claims.Email, email)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, _ := GenerateToken("secret-a", 1, "a@b.com", 24)
	_, err := ValidateToken("secret-b", token)
	if err == nil {
		t.Error("expected error validating with wrong secret")
	}
}

func TestValidateToken_Garbage(t *testing.T) {
	_, err := ValidateToken("secret", "not-a-jwt")
	if err == nil {
		t.Error("expected error for garbage token")
	}
}

// TestValidateToken_RotationWindow covers the dual-key behaviour: a token signed
// under an old secret keeps validating while that secret is in the previous list,
// and stops once it is dropped.
func TestValidateToken_RotationWindow(t *testing.T) {
	oldSecret := "old-secret-key-at-least-32-chars-long"
	newSecret := "new-secret-key-at-least-32-chars-long"

	// Token issued before the rotation, signed with the old secret.
	oldToken, err := GenerateToken(oldSecret, 7, "u@b.com", 24)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// During the overlap window: new secret is primary, old is verify-only.
	claims, err := ValidateToken(newSecret, oldToken, oldSecret)
	if err != nil {
		t.Fatalf("old token should validate during overlap window: %v", err)
	}
	if claims.UserID != 7 {
		t.Errorf("UserID = %d, want 7", claims.UserID)
	}

	// After the window: old secret dropped, only the new primary remains.
	if _, err := ValidateToken(newSecret, oldToken); err == nil {
		t.Error("old token should fail once the old secret is dropped")
	}

	// New tokens are signed with the primary and validate without any previous.
	newToken, _ := GenerateToken(newSecret, 7, "u@b.com", 24)
	if _, err := ValidateToken(newSecret, newToken); err != nil {
		t.Errorf("new token should validate under primary: %v", err)
	}
}
