package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrTokenNotFound = errors.New("token not found")
	ErrTokenRevoked  = errors.New("token has been revoked")
	ErrTokenExpired  = errors.New("token has expired")
)

// Token represents an API token record.
type Token struct {
	ID               int64
	UUID             string
	Name             string
	Description      string
	TokenHash        string
	UserID           int64
	Permissions      string // JSON array
	Department       sql.NullString
	MaxFileSizeBytes sql.NullInt64
	AllowedFileTypes sql.NullString
	ExpiresAt        sql.NullTime
	RevokedAt        sql.NullTime
	RevokedBy        sql.NullInt64
	RevokeReason     sql.NullString
	UsageCount       int64
	LastUsedAt       sql.NullTime
	CreatedAt        time.Time
}

// TokenService handles API token operations.
type TokenService struct {
	db *database.DB
}

func NewTokenService(db *database.DB) *TokenService {
	return &TokenService{db: db}
}

// Create generates a new API token. Returns the raw secret (shown once) and the token record.
func (s *TokenService) Create(
	userID int64,
	name, description string,
	permissions string,
	department string,
	maxFileSize *int64,
	allowedTypes string,
	expiresAt *time.Time,
) (secret string, token *Token, err error) {
	// Generate 32 random bytes → 64-char hex secret
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	secret = "ind_" + hex.EncodeToString(raw) // prefix for easy identification

	// Bcrypt hash the secret for storage
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), 10)
	if err != nil {
		return "", nil, err
	}

	tokenUUID := uuid.New().String()

	var dept sql.NullString
	if department != "" {
		dept = sql.NullString{String: department, Valid: true}
	}
	var maxSize sql.NullInt64
	if maxFileSize != nil {
		maxSize = sql.NullInt64{Int64: *maxFileSize, Valid: true}
	}
	var types sql.NullString
	if allowedTypes != "" {
		types = sql.NullString{String: allowedTypes, Valid: true}
	}
	var expires sql.NullTime
	if expiresAt != nil {
		expires = sql.NullTime{Time: *expiresAt, Valid: true}
	}

	if permissions == "" {
		permissions = `["read"]`
	}

	var id int64
	err = s.db.QueryRow(
		`INSERT INTO api_tokens (uuid, name, description, token_hash, user_id, permissions, department, max_file_size_bytes, allowed_file_types, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		tokenUUID, name, description, string(hash), userID, permissions, dept, maxSize, types, expires,
	).Scan(&id)
	if err != nil {
		return "", nil, err
	}

	token, err = s.GetByID(id)
	return secret, token, err
}

// GetByID retrieves a token by its database ID.
func (s *TokenService) GetByID(id int64) (*Token, error) {
	t := &Token{}
	err := s.db.QueryRow(
		`SELECT id, uuid, name, description, token_hash, user_id, permissions, department,
		        max_file_size_bytes, allowed_file_types, expires_at, revoked_at, revoked_by,
		        revoke_reason, usage_count, last_used_at, created_at
		 FROM api_tokens WHERE id = ?`, id,
	).Scan(
		&t.ID, &t.UUID, &t.Name, &t.Description, &t.TokenHash, &t.UserID, &t.Permissions,
		&t.Department, &t.MaxFileSizeBytes, &t.AllowedFileTypes, &t.ExpiresAt, &t.RevokedAt,
		&t.RevokedBy, &t.RevokeReason, &t.UsageCount, &t.LastUsedAt, &t.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrTokenNotFound
	}
	return t, err
}

// ValidateSecret finds a token by trying bcrypt compare against all non-revoked,
// non-expired tokens. Returns the token if found and valid.
// This is O(n) on active tokens — acceptable for typical deployments (<1000 tokens).
func (s *TokenService) ValidateSecret(secret string) (*Token, error) {
	rows, err := s.db.Query(
		`SELECT id, uuid, name, description, token_hash, user_id, permissions, department,
		        max_file_size_bytes, allowed_file_types, expires_at, revoked_at, revoked_by,
		        revoke_reason, usage_count, last_used_at, created_at
		 FROM api_tokens
		 WHERE revoked_at IS NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		t := &Token{}
		if err := rows.Scan(
			&t.ID, &t.UUID, &t.Name, &t.Description, &t.TokenHash, &t.UserID, &t.Permissions,
			&t.Department, &t.MaxFileSizeBytes, &t.AllowedFileTypes, &t.ExpiresAt, &t.RevokedAt,
			&t.RevokedBy, &t.RevokeReason, &t.UsageCount, &t.LastUsedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}

		// Check expiry
		if t.ExpiresAt.Valid && time.Now().After(t.ExpiresAt.Time) {
			continue
		}

		// Compare secret against hash
		if bcrypt.CompareHashAndPassword([]byte(t.TokenHash), []byte(secret)) == nil {
			return t, nil
		}
	}

	return nil, ErrTokenNotFound
}

// RecordUsage increments usage count and updates last_used_at.
func (s *TokenService) RecordUsage(tokenID int64) {
	_, _ = s.db.Exec(
		`UPDATE api_tokens SET usage_count = usage_count + 1, last_used_at = CURRENT_TIMESTAMP WHERE id = ?`,
		tokenID,
	)
}

// LogUsage records a detailed usage entry.
func (s *TokenService) LogUsage(tokenID int64, endpoint, method, ip, userAgent string, statusCode int) {
	_, _ = s.db.Exec(
		`INSERT INTO token_usage_log (token_id, endpoint, method, ip_address, user_agent, status_code)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		tokenID, endpoint, method, ip, userAgent, statusCode,
	)
}

// ListByUser returns all tokens owned by a user (including revoked, for audit).
func (s *TokenService) ListByUser(userID int64) ([]*Token, error) {
	rows, err := s.db.Query(
		`SELECT id, uuid, name, description, token_hash, user_id, permissions, department,
		        max_file_size_bytes, allowed_file_types, expires_at, revoked_at, revoked_by,
		        revoke_reason, usage_count, last_used_at, created_at
		 FROM api_tokens WHERE user_id = ?
		 ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTokens(rows)
}

// ListAll returns all tokens (admin view) with pagination.
func (s *TokenService) ListAll(limit, offset int) ([]*Token, int64, error) {
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_tokens`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		`SELECT id, uuid, name, description, token_hash, user_id, permissions, department,
		        max_file_size_bytes, allowed_file_types, expires_at, revoked_at, revoked_by,
		        revoke_reason, usage_count, last_used_at, created_at
		 FROM api_tokens
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tokens, err := scanTokens(rows)
	return tokens, total, err
}

// Revoke soft-deletes a token by setting revoked_at.
func (s *TokenService) Revoke(tokenID, revokedBy int64, reason string) error {
	result, err := s.db.Exec(
		`UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP, revoked_by = ?, revoke_reason = ?
		 WHERE id = ? AND revoked_at IS NULL`,
		revokedBy, reason, tokenID,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// RevokeAllByUser revokes all active tokens for a user. Used when deactivating accounts.
func (s *TokenService) RevokeAllByUser(userID, revokedBy int64, reason string) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP, revoked_by = ?, revoke_reason = ?
		 WHERE user_id = ? AND revoked_at IS NULL`,
		revokedBy, reason, userID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// BulkRevoke revokes multiple tokens by ID.
func (s *TokenService) BulkRevoke(tokenIDs []int64, revokedBy int64, reason string) (int64, error) {
	if len(tokenIDs) == 0 {
		return 0, nil
	}

	// Build placeholder string
	placeholders := make([]byte, 0, len(tokenIDs)*2)
	args := make([]any, 0, len(tokenIDs)+2)
	args = append(args, revokedBy, reason)
	for i, id := range tokenIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}

	query := `UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP, revoked_by = ?, revoke_reason = ?
	          WHERE id IN (` + string(placeholders) + `) AND revoked_at IS NULL`

	result, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetByUUID retrieves a token by its public UUID.
func (s *TokenService) GetByUUID(tokenUUID string) (*Token, error) {
	t := &Token{}
	err := s.db.QueryRow(
		`SELECT id, uuid, name, description, token_hash, user_id, permissions, department,
		        max_file_size_bytes, allowed_file_types, expires_at, revoked_at, revoked_by,
		        revoke_reason, usage_count, last_used_at, created_at
		 FROM api_tokens WHERE uuid = ?`, tokenUUID,
	).Scan(
		&t.ID, &t.UUID, &t.Name, &t.Description, &t.TokenHash, &t.UserID, &t.Permissions,
		&t.Department, &t.MaxFileSizeBytes, &t.AllowedFileTypes, &t.ExpiresAt, &t.RevokedAt,
		&t.RevokedBy, &t.RevokeReason, &t.UsageCount, &t.LastUsedAt, &t.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrTokenNotFound
	}
	return t, err
}

func scanTokens(rows *sql.Rows) ([]*Token, error) {
	var tokens []*Token
	for rows.Next() {
		t := &Token{}
		if err := rows.Scan(
			&t.ID, &t.UUID, &t.Name, &t.Description, &t.TokenHash, &t.UserID, &t.Permissions,
			&t.Department, &t.MaxFileSizeBytes, &t.AllowedFileTypes, &t.ExpiresAt, &t.RevokedAt,
			&t.RevokedBy, &t.RevokeReason, &t.UsageCount, &t.LastUsedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
