package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT payload for session tokens.
type Claims struct {
	UserID int64  `json:"uid"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed JWT for the given user.
func GenerateToken(secret string, userID int64, email string, expiryHours int) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expiryHours) * time.Hour)),
			Issuer:    "indelible",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken parses and validates a JWT, returning the claims if valid.
//
// Tokens are always signed with the primary secret. Any previous secrets are
// verify-only: they let tokens already issued under an old secret keep working
// across a secret rotation until they expire. The primary is tried first, then
// each previous secret in turn; the token is valid if any of them verifies.
// See docs/guides/key-rotation.md for the rotation procedure and overlap window.
func ValidateToken(primary string, tokenString string, previous ...string) (*Claims, error) {
	// Primary first (the common case and the most meaningful error to surface),
	// then each previous secret for the rotation overlap window.
	var firstErr error
	for _, secret := range append([]string{primary}, previous...) {
		claims, err := parseWithSecret(secret, tokenString)
		if err == nil {
			return claims, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

// parseWithSecret verifies a token against exactly one HMAC secret.
func parseWithSecret(secret string, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}
