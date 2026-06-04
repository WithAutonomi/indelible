package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the production work factor. It is deliberately high (12) for
// real deployments. Under `go test` it is lowered to bcrypt.MinCost: the
// handler suite performs hundreds of hashes/compares, and at cost 12 that work
// dominates the runtime — enough to blow the race-detector job past its 15m
// timeout. testing.Testing() (Go 1.21+) reports false in the production binary,
// so prod always uses cost 12.
var bcryptCost = func() int {
	if testing.Testing() {
		return bcrypt.MinCost
	}
	return 12
}()

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(bytes), err
}

// CheckPassword compares a plaintext password against a bcrypt hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
