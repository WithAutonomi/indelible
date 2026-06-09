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

// dummyHash is a bcrypt hash computed once at the package's cost factor. It lets
// the login path perform a real bcrypt comparison even when the email is
// unknown, so response time doesn't reveal whether an account exists. The
// plaintext is irrelevant — any compare against it fails.
var dummyHash = func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("constant-time-login-placeholder"), bcryptCost)
	if err != nil {
		// GenerateFromPassword only errors on an out-of-range cost, which can't
		// happen with the fixed cost values above.
		panic("auth: failed to precompute dummy bcrypt hash: " + err.Error())
	}
	return string(h)
}()

// DummyCheckPassword runs a bcrypt comparison against a fixed internal hash and
// discards the result. Call it on the unknown-email login path so the handler
// spends roughly the same time as a real wrong-password compare, defeating a
// timing oracle that would otherwise reveal which emails exist. (V2-430)
func DummyCheckPassword(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
}
