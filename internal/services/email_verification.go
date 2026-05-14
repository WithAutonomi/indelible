package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

// EmailVerificationService manages email verification tokens.
type EmailVerificationService struct {
	db *database.DB
}

func NewEmailVerificationService(db *database.DB) *EmailVerificationService {
	return &EmailVerificationService{db: db}
}

// Create generates a one-time verification token for the given user.
// Returns the raw token (to be included in the verification URL).
// Tokens expire after 24 hours.
func (s *EmailVerificationService) Create(userID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)

	// Invalidate any existing unused tokens for this user
	_, _ = s.db.Exec(
		`UPDATE email_verification_tokens SET used_at = CURRENT_TIMESTAMP WHERE user_id = ? AND used_at IS NULL`,
		userID,
	)

	_, err := s.db.Exec(
		`INSERT INTO email_verification_tokens (user_id, token, expires_at)
		 VALUES (?, ?, ?)`,
		userID, token, time.Now().Add(24*time.Hour),
	)
	return token, err
}

// Validate checks a verification token. Returns the user ID if valid.
// Marks the token as used and sets the user's email_verified flag.
func (s *EmailVerificationService) Validate(token string) (int64, error) {
	var userID int64
	var expiresAt time.Time
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM email_verification_tokens
		 WHERE token = ? AND used_at IS NULL`,
		token,
	).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}

	if time.Now().After(expiresAt) {
		return 0, ErrInvalidCredentials
	}

	// Mark token as used
	_, _ = s.db.Exec(
		`UPDATE email_verification_tokens SET used_at = CURRENT_TIMESTAMP WHERE token = ?`,
		token,
	)

	// Set email_verified on the user
	_, err = s.db.Exec(
		`UPDATE users SET email_verified = TRUE WHERE id = ? AND deleted_at IS NULL`,
		userID,
	)

	return userID, err
}
