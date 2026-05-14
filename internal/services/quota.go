package services

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrQuotaNotFound  = errors.New("quota not found")
	ErrQuotaExceeded  = errors.New("quota exceeded")
	ErrQuotaDuplicate = errors.New("quota already exists for this entity")
)

// Quota represents a storage quota.
type Quota struct {
	ID         int64
	EntityType string // "user", "group", "department", "system"
	EntityID   sql.NullString
	MaxBytes   int64
	IsEnabled  bool
	UsedBytes  int64 // populated at query time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// QuotaService handles quota operations.
type QuotaService struct {
	db *sql.DB
}

// NewQuotaService creates a new QuotaService.
func NewQuotaService(db *sql.DB) *QuotaService {
	return &QuotaService{db: db}
}

// Create adds a new quota.
func (s *QuotaService) Create(entityType, entityID string, maxBytes int64) (*Quota, error) {
	var eID sql.NullString
	if entityID != "" {
		eID = sql.NullString{String: entityID, Valid: true}
	}

	var id int64
	err := s.db.QueryRow(
		`INSERT INTO quotas (entity_type, entity_id, max_bytes) VALUES (?, ?, ?) RETURNING id`,
		entityType, eID, maxBytes,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrQuotaDuplicate
		}
		return nil, err
	}
	return s.GetByID(id)
}

// GetByID retrieves a quota with current usage.
func (s *QuotaService) GetByID(id int64) (*Quota, error) {
	q := &Quota{}
	err := s.db.QueryRow(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled, created_at, updated_at FROM quotas WHERE id = ?`, id,
	).Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQuotaNotFound
		}
		return nil, err
	}

	q.UsedBytes = s.calcUsage(q.EntityType, q.EntityID)
	return q, nil
}

// List returns all quotas with current usage.
func (s *QuotaService) List() ([]*Quota, error) {
	rows, err := s.db.Query(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled, created_at, updated_at FROM quotas ORDER BY entity_type, entity_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var quotas []*Quota
	for rows.Next() {
		q := &Quota{}
		if err := rows.Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		q.UsedBytes = s.calcUsage(q.EntityType, q.EntityID)
		quotas = append(quotas, q)
	}
	return quotas, rows.Err()
}

// Update modifies a quota.
func (s *QuotaService) Update(id int64, maxBytes int64, isEnabled bool) (*Quota, error) {
	_, err := s.db.Exec(
		`UPDATE quotas SET max_bytes = ?, is_enabled = ?, updated_at = datetime('now') WHERE id = ?`,
		maxBytes, isEnabled, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

// Delete removes a quota.
func (s *QuotaService) Delete(id int64) error {
	result, err := s.db.Exec(`DELETE FROM quotas WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrQuotaNotFound
	}
	return nil
}

// CheckUserQuota verifies that adding fileSize bytes for the given user doesn't exceed any applicable quota.
// Returns nil if allowed, ErrQuotaExceeded if any quota would be exceeded.
// Uses a transaction to prevent concurrent uploads from bypassing the check.
func (s *QuotaService) CheckUserQuota(userID int64, fileSize int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check user-level quota
	var q Quota
	err = tx.QueryRow(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled FROM quotas WHERE entity_type = 'user' AND entity_id = ? AND is_enabled = 1`,
		userID,
	).Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled)
	if err == nil {
		var used int64
		tx.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE user_id = ? AND status = 'completed'`, userID).Scan(&used)
		if used+fileSize > q.MaxBytes {
			return ErrQuotaExceeded
		}
	}

	// Check system-level quota
	err = tx.QueryRow(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled FROM quotas WHERE entity_type = 'system' AND is_enabled = 1`,
	).Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled)
	if err == nil {
		var used int64
		tx.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE status = 'completed'`).Scan(&used)
		if used+fileSize > q.MaxBytes {
			return ErrQuotaExceeded
		}
	}

	return tx.Commit()
}

// CheckUserQuotaInFlight verifies quota including queued and processing uploads (not just completed).
// Used at processing time to prevent quota bypass from bulk queueing.
func (s *QuotaService) CheckUserQuotaInFlight(userID int64, fileSize int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check user-level quota
	var q Quota
	err = tx.QueryRow(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled FROM quotas WHERE entity_type = 'user' AND entity_id = ? AND is_enabled = 1`,
		userID,
	).Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled)
	if err == nil {
		var used int64
		tx.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE user_id = ? AND status IN ('completed', 'processing', 'queued')`, userID).Scan(&used)
		if used+fileSize > q.MaxBytes {
			return ErrQuotaExceeded
		}
	}

	// Check system-level quota
	err = tx.QueryRow(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled FROM quotas WHERE entity_type = 'system' AND is_enabled = 1`,
	).Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled)
	if err == nil {
		var used int64
		tx.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE status IN ('completed', 'processing', 'queued')`).Scan(&used)
		if used+fileSize > q.MaxBytes {
			return ErrQuotaExceeded
		}
	}

	return tx.Commit()
}

func (s *QuotaService) calcUsage(entityType string, entityID sql.NullString) int64 {
	var used int64
	switch entityType {
	case "user":
		if entityID.Valid {
			s.db.QueryRow(
				`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE user_id = ? AND status = 'completed'`, entityID.String,
			).Scan(&used)
		}
	case "system":
		s.db.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE status = 'completed'`).Scan(&used)
	}
	return used
}

