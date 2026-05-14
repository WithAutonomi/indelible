package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountInactive    = errors.New("account is inactive")
)

// User represents a user record from the database.
type User struct {
	ID                int64
	Email             string
	PasswordHash      sql.NullString
	FirstName         string
	LastName          string
	IsActive          bool
	IsServiceAccount  bool
	EmailVerified     bool
	ExternalID        sql.NullString
	LastLoginAt       sql.NullTime
	MaxFileSizeBytes  sql.NullInt64
	AllowedFileTypes  sql.NullString // JSON array
	PasswordChangedAt sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         sql.NullTime
}

// UserService handles user-related database operations.
type UserService struct {
	db *database.DB
}

func NewUserService(db *database.DB) *UserService {
	return &UserService{db: db}
}

// Create inserts a new user and returns the created record.
func (s *UserService) Create(email, passwordHash, firstName, lastName string) (*User, error) {
	var id int64
	err := s.db.QueryRow(
		`INSERT INTO users (email, password_hash, first_name, last_name)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		email, passwordHash, firstName, lastName,
	).Scan(&id)
	if err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	return s.GetByID(id)
}

// CreateServiceAccount inserts a service account (no password, can't login).
func (s *UserService) CreateServiceAccount(email, firstName, lastName string) (*User, error) {
	var id int64
	err := s.db.QueryRow(
		`INSERT INTO users (email, first_name, last_name, is_service_account)
		 VALUES (?, ?, ?, TRUE)
		 RETURNING id`,
		email, firstName, lastName,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return s.GetByID(id)
}

// GetByID retrieves a user by ID (excluding soft-deleted).
func (s *UserService) GetByID(id int64) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, email, password_hash, first_name, last_name, is_active,
		        is_service_account, email_verified, external_id, last_login_at,
		        max_file_size_bytes, allowed_file_types, password_changed_at,
		        created_at, updated_at, deleted_at
		 FROM users WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.IsActive,
		&u.IsServiceAccount, &u.EmailVerified, &u.ExternalID, &u.LastLoginAt,
		&u.MaxFileSizeBytes, &u.AllowedFileTypes, &u.PasswordChangedAt,
		&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return u, err
}

// GetByEmail retrieves a user by email (excluding soft-deleted).
func (s *UserService) GetByEmail(email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, email, password_hash, first_name, last_name, is_active,
		        is_service_account, email_verified, external_id, last_login_at,
		        max_file_size_bytes, allowed_file_types, password_changed_at,
		        created_at, updated_at, deleted_at
		 FROM users WHERE email = ? AND deleted_at IS NULL`,
		email,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.IsActive,
		&u.IsServiceAccount, &u.EmailVerified, &u.ExternalID, &u.LastLoginAt,
		&u.MaxFileSizeBytes, &u.AllowedFileTypes, &u.PasswordChangedAt,
		&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return u, err
}

// UpdateLastLogin sets the last_login_at timestamp for a user.
func (s *UserService) UpdateLastLogin(id int64) error {
	_, err := s.db.Exec(
		`UPDATE users SET last_login_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	return err
}

// Count returns the total number of active (non-deleted) users.
func (s *UserService) Count() (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&count)
	return count, err
}

// List returns paginated users (excluding soft-deleted).
func (s *UserService) List(limit, offset int) ([]*User, int64, error) {
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		`SELECT id, email, password_hash, first_name, last_name, is_active,
		        is_service_account, email_verified, external_id, last_login_at,
		        max_file_size_bytes, allowed_file_types, password_changed_at,
		        created_at, updated_at, deleted_at
		 FROM users WHERE deleted_at IS NULL
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.IsActive,
			&u.IsServiceAccount, &u.EmailVerified, &u.ExternalID, &u.LastLoginAt,
			&u.MaxFileSizeBytes, &u.AllowedFileTypes, &u.PasswordChangedAt,
			&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
		); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// Update modifies user fields. Only non-zero values are updated.
func (s *UserService) Update(id int64, firstName, lastName string, isActive *bool) error {
	u, err := s.GetByID(id)
	if err != nil {
		return err
	}
	if firstName != "" {
		u.FirstName = firstName
	}
	if lastName != "" {
		u.LastName = lastName
	}
	active := u.IsActive
	if isActive != nil {
		active = *isActive
	}

	_, err = s.db.Exec(
		`UPDATE users SET first_name = ?, last_name = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		u.FirstName, u.LastName, active, id,
	)
	return err
}

// SoftDelete marks a user as deleted.
func (s *UserService) SoftDelete(id int64) error {
	_, err := s.db.Exec(
		`UPDATE users SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	return err
}

// UpdatePassword changes a user's password hash and records the timestamp
// so that JWTs issued before the change are rejected.
func (s *UserService) UpdatePassword(id int64, passwordHash string) error {
	_, err := s.db.Exec(
		`UPDATE users SET password_hash = ?, password_changed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		passwordHash, id,
	)
	return err
}

// GetByExternalID retrieves a user by their SCIM external ID.
func (s *UserService) GetByExternalID(externalID string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, email, password_hash, first_name, last_name, is_active,
		        is_service_account, email_verified, external_id, last_login_at,
		        max_file_size_bytes, allowed_file_types, password_changed_at,
		        created_at, updated_at, deleted_at
		 FROM users WHERE external_id = ? AND deleted_at IS NULL`,
		externalID,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.IsActive,
		&u.IsServiceAccount, &u.EmailVerified, &u.ExternalID, &u.LastLoginAt,
		&u.MaxFileSizeBytes, &u.AllowedFileTypes, &u.PasswordChangedAt,
		&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return u, err
}

// CreateFromSCIM creates a SCIM-provisioned user (no password, not a service account).
func (s *UserService) CreateFromSCIM(email, firstName, lastName, externalID string) (*User, error) {
	var extID sql.NullString
	if externalID != "" {
		extID = sql.NullString{String: externalID, Valid: true}
	}
	var id int64
	err := s.db.QueryRow(
		`INSERT INTO users (email, first_name, last_name, external_id)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		email, firstName, lastName, extID,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return s.GetByID(id)
}

// UpdateFromSCIM performs a full SCIM replace on a user record.
func (s *UserService) UpdateFromSCIM(id int64, email, firstName, lastName string, externalID *string, isActive *bool) error {
	var extID sql.NullString
	if externalID != nil {
		extID = sql.NullString{String: *externalID, Valid: *externalID != ""}
	}

	_, err := s.db.Exec(
		`UPDATE users SET email = ?, first_name = ?, last_name = ?, external_id = ?,
		        is_active = COALESCE(?, is_active), updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND deleted_at IS NULL`,
		email, firstName, lastName, extID, isActive, id,
	)
	return err
}

// SetExternalID sets the SCIM external ID for a user.
func (s *UserService) SetExternalID(id int64, externalID string) error {
	_, err := s.db.Exec(
		`UPDATE users SET external_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		externalID, id,
	)
	return err
}

// isUniqueViolation checks if an error is a unique constraint violation.
// Works for both SQLite and PostgreSQL.
func isUniqueViolation(err error) bool {
	msg := err.Error()
	// SQLite: "UNIQUE constraint failed: ..."
	// PostgreSQL: "duplicate key value violates unique constraint ..."
	return contains(msg, "UNIQUE constraint failed") || contains(msg, "duplicate key")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
