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
