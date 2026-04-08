package auth

import "testing"

func FuzzValidateToken(f *testing.F) {
	// Seed with a valid token
	token, _ := GenerateToken("test-secret-at-least-32-chars-long", 1, "test@example.com", 24)
	f.Add(token)
	// Malformed tokens
	f.Add("")
	f.Add("not-a-jwt")
	f.Add("eyJhbGciOiJIUzI1NiJ9.e30.invalid")
	f.Add("a.b.c")
	f.Add("eyJhbGciOiJub25lIn0.eyJ1aWQiOjF9.") // alg:none attack

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic on any input
		_, _ = ValidateToken("test-secret-at-least-32-chars-long", input)
	})
}
