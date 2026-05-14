package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrScimTokenNotFound = errors.New("SCIM token not found")
)

// ScimToken represents a SCIM bearer token record.
type ScimToken struct {
	ID         int64
	Name       string
	TokenHash  string
	IsActive   bool
	CreatedBy  int64
	LastUsedAt sql.NullTime
	CreatedAt  time.Time
	RevokedAt  sql.NullTime
}

// ScimTokenService handles SCIM token operations.
type ScimTokenService struct {
	db *database.DB
}

func NewScimTokenService(db *database.DB) *ScimTokenService {
	return &ScimTokenService{db: db}
}

// Create generates a new SCIM token. Returns the raw secret (shown once) and the token record.
func (s *ScimTokenService) Create(name string, createdBy int64) (secret string, token *ScimToken, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	secret = "scim_" + hex.EncodeToString(raw)

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), 10)
	if err != nil {
		return "", nil, err
	}

	var id int64
	err = s.db.QueryRow(
		`INSERT INTO scim_tokens (name, token_hash, created_by)
		 VALUES (?, ?, ?)
		 RETURNING id`,
		name, string(hash), createdBy,
	).Scan(&id)
	if err != nil {
		return "", nil, err
	}

	token, err = s.GetByID(id)
	return secret, token, err
}

// GetByID retrieves a SCIM token by ID.
func (s *ScimTokenService) GetByID(id int64) (*ScimToken, error) {
	t := &ScimToken{}
	err := s.db.QueryRow(
		`SELECT id, name, token_hash, is_active, created_by, last_used_at, created_at, revoked_at
		 FROM scim_tokens WHERE id = ?`, id,
	).Scan(&t.ID, &t.Name, &t.TokenHash, &t.IsActive, &t.CreatedBy, &t.LastUsedAt, &t.CreatedAt, &t.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, ErrScimTokenNotFound
	}
	return t, err
}

// Validate finds a SCIM token by bcrypt comparison against all active tokens.
func (s *ScimTokenService) Validate(secret string) (*ScimToken, error) {
	rows, err := s.db.Query(
		`SELECT id, name, token_hash, is_active, created_by, last_used_at, created_at, revoked_at
		 FROM scim_tokens
		 WHERE is_active = TRUE AND revoked_at IS NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		t := &ScimToken{}
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.IsActive, &t.CreatedBy, &t.LastUsedAt, &t.CreatedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(t.TokenHash), []byte(secret)) == nil {
			return t, nil
		}
	}

	return nil, ErrScimTokenNotFound
}

// List returns all SCIM tokens (including revoked, for audit).
func (s *ScimTokenService) List() ([]*ScimToken, error) {
	rows, err := s.db.Query(
		`SELECT id, name, token_hash, is_active, created_by, last_used_at, created_at, revoked_at
		 FROM scim_tokens ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*ScimToken
	for rows.Next() {
		t := &ScimToken{}
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.IsActive, &t.CreatedBy, &t.LastUsedAt, &t.CreatedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// Revoke marks a SCIM token as revoked.
func (s *ScimTokenService) Revoke(id int64) error {
	result, err := s.db.Exec(
		`UPDATE scim_tokens SET is_active = FALSE, revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrScimTokenNotFound
	}
	return nil
}

// RecordUsage updates the last_used_at timestamp.
func (s *ScimTokenService) RecordUsage(id int64) {
	_, _ = s.db.Exec(
		`UPDATE scim_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
}
