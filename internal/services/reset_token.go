package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

// ResetTokenService manages password reset tokens.
type ResetTokenService struct {
	db *database.DB
}

func NewResetTokenService(db *database.DB) *ResetTokenService {
	return &ResetTokenService{db: db}
}

// Create generates a one-time-use reset token for the given email.
// Returns the raw token (to be sent to the user). The token hash is stored.
func (s *ResetTokenService) Create(userID int64) (string, error) {
	// Generate 32 random bytes → 64-char hex token
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)

	// Invalidate any existing tokens for this user
	_, _ = s.db.Exec(
		`UPDATE password_reset_tokens SET used_at = CURRENT_TIMESTAMP WHERE user_id = ? AND used_at IS NULL`,
		userID,
	)

	// Store new token (plain — these are short-lived and single-use)
	_, err := s.db.Exec(
		`INSERT INTO password_reset_tokens (user_id, token, expires_at)
		 VALUES (?, ?, ?)`,
		userID, token, time.Now().Add(1*time.Hour),
	)
	return token, err
}

// Validate checks a reset token. Returns the user ID if valid.
// Marks the token as used (one-time).
func (s *ResetTokenService) Validate(token string) (int64, error) {
	var userID int64
	var expiresAt time.Time
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM password_reset_tokens
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

	// Mark as used
	_, _ = s.db.Exec(
		`UPDATE password_reset_tokens SET used_at = CURRENT_TIMESTAMP WHERE token = ?`,
		token,
	)

	return userID, nil
}
