package auth

import (
	"testing"
)

func TestHashAndCheckPassword(t *testing.T) {
	password := "mySecurePassword123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !CheckPassword(password, hash) {
		t.Error("CheckPassword returned false for correct password")
	}

	if CheckPassword("wrongPassword", hash) {
		t.Error("CheckPassword returned true for wrong password")
	}
}
