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

// TestDummyCheckPassword asserts the constant-time login helper is callable
// without panicking — the dummy hash must have precomputed successfully at
// init. There's nothing to assert about its result (it exists only to consume
// bcrypt time on the unknown-email path); the value is its side-effect timing.
func TestDummyCheckPassword(t *testing.T) {
	DummyCheckPassword("anything")
	DummyCheckPassword("")
}
