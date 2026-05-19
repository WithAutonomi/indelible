package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
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
	db *database.DB
}

// NewQuotaService creates a new QuotaService.
func NewQuotaService(db *database.DB) *QuotaService {
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
		`UPDATE quotas SET max_bytes = ?, is_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
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

// quotaCompletedStatuses is the status filter for "real" disk usage (post-upload).
// quotaInFlightStatuses also counts queued + processing so bulk-queueing can't
// bypass a quota — used at the worker's pre-processing recheck.
var (
	quotaCompletedStatuses = []string{"completed"}
	quotaInFlightStatuses  = []string{"completed", "processing", "queued"}
)

// CheckUserQuota verifies that adding fileSize bytes for the given user doesn't
// exceed any applicable quota tier (user, group, department, system). Returns
// nil if allowed, ErrQuotaExceeded if any tier would be exceeded. tokenID is
// optional — pass nil for uploads not tied to an API token (e.g., web UI),
// which means the department tier is skipped.
//
// Only completed uploads count; use CheckUserQuotaInFlight at processing time
// to also reject bulk-queueing-then-quota-bypass.
func (s *QuotaService) CheckUserQuota(userID int64, tokenID *int64, fileSize int64) error {
	return s.checkAllTiers(userID, tokenID, fileSize, quotaCompletedStatuses)
}

// CheckUserQuotaInFlight is CheckUserQuota with queued and processing uploads
// also counted toward usage. Use at the worker, not at upload accept time.
func (s *QuotaService) CheckUserQuotaInFlight(userID int64, tokenID *int64, fileSize int64) error {
	return s.checkAllTiers(userID, tokenID, fileSize, quotaInFlightStatuses)
}

// checkAllTiers evaluates user/group/department/system tiers under a single
// transaction so concurrent uploads can't slip past a shared quota by racing.
// First failing tier wins — callers don't need to know which tier rejected,
// the error message is the same.
func (s *QuotaService) checkAllTiers(userID int64, tokenID *int64, fileSize int64, statuses []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkUserTier(tx, userID, fileSize, statuses); err != nil {
		return err
	}
	if err := s.checkGroupTier(tx, userID, fileSize, statuses); err != nil {
		return err
	}
	if err := s.checkDepartmentTier(tx, tokenID, fileSize, statuses); err != nil {
		return err
	}
	if err := s.checkSystemTier(tx, fileSize, statuses); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *QuotaService) checkUserTier(tx *database.Tx, userID, fileSize int64, statuses []string) error {
	maxBytes, ok, err := lookupQuotaLimit(tx, "user", sql.NullString{String: int64ToString(userID), Valid: true})
	if err != nil || !ok {
		return err
	}
	var used int64
	if err := tx.QueryRow(
		`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE user_id = ? AND status IN (`+placeholders(len(statuses))+`)`,
		appendInterfaces(userID, statuses)...,
	).Scan(&used); err != nil {
		return err
	}
	if used+fileSize > maxBytes {
		return ErrQuotaExceeded
	}
	return nil
}

func (s *QuotaService) checkSystemTier(tx *database.Tx, fileSize int64, statuses []string) error {
	maxBytes, ok, err := lookupQuotaLimit(tx, "system", sql.NullString{})
	if err != nil || !ok {
		return err
	}
	var used int64
	if err := tx.QueryRow(
		`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE status IN (`+placeholders(len(statuses))+`)`,
		stringSliceToInterfaces(statuses)...,
	).Scan(&used); err != nil {
		return err
	}
	if used+fileSize > maxBytes {
		return ErrQuotaExceeded
	}
	return nil
}

// checkGroupTier rejects if any group the user belongs to has a quota whose
// aggregate usage (summed across every member's uploads) would be exceeded.
// First failing group wins.
func (s *QuotaService) checkGroupTier(tx *database.Tx, userID, fileSize int64, statuses []string) error {
	rows, err := tx.Query(
		`SELECT q.entity_id, q.max_bytes
		   FROM quotas q
		   JOIN group_members gm ON gm.group_id = CAST(q.entity_id AS INTEGER)
		  WHERE q.entity_type = 'group'
		    AND q.is_enabled = TRUE
		    AND q.entity_id IS NOT NULL
		    AND gm.user_id = ?`,
		userID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type groupQuota struct {
		groupID  string
		maxBytes int64
	}
	var quotas []groupQuota
	for rows.Next() {
		var gq groupQuota
		var gid sql.NullString
		if err := rows.Scan(&gid, &gq.maxBytes); err != nil {
			return err
		}
		if !gid.Valid {
			continue
		}
		gq.groupID = gid.String
		quotas = append(quotas, gq)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, gq := range quotas {
		var used int64
		args := append([]interface{}{gq.groupID}, stringSliceToInterfaces(statuses)...)
		if err := tx.QueryRow(
			`SELECT COALESCE(SUM(u.file_size), 0)
			   FROM uploads u
			   JOIN group_members gm ON gm.user_id = u.user_id
			  WHERE gm.group_id = CAST(? AS INTEGER)
			    AND u.status IN (`+placeholders(len(statuses))+`)`,
			args...,
		).Scan(&used); err != nil {
			return err
		}
		if used+fileSize > gq.maxBytes {
			return ErrQuotaExceeded
		}
	}
	return nil
}

// checkDepartmentTier rejects if the API token used for this upload belongs to
// a department whose aggregate usage (summed across uploads via any token in
// that department) would be exceeded. No-op when tokenID is nil (web UI
// upload), when the token has no department set, or when no department quota
// is configured for that department.
func (s *QuotaService) checkDepartmentTier(tx *database.Tx, tokenID *int64, fileSize int64, statuses []string) error {
	if tokenID == nil {
		return nil
	}
	var dept sql.NullString
	if err := tx.QueryRow(
		`SELECT department FROM api_tokens WHERE id = ?`, *tokenID,
	).Scan(&dept); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if !dept.Valid || dept.String == "" {
		return nil
	}

	maxBytes, ok, err := lookupQuotaLimit(tx, "department", dept)
	if err != nil || !ok {
		return err
	}

	var used int64
	args := append([]interface{}{dept.String}, stringSliceToInterfaces(statuses)...)
	if err := tx.QueryRow(
		`SELECT COALESCE(SUM(u.file_size), 0)
		   FROM uploads u
		   JOIN api_tokens t ON t.id = u.token_id
		  WHERE t.department = ?
		    AND u.status IN (`+placeholders(len(statuses))+`)`,
		args...,
	).Scan(&used); err != nil {
		return err
	}
	if used+fileSize > maxBytes {
		return ErrQuotaExceeded
	}
	return nil
}

// lookupQuotaLimit returns (maxBytes, true, nil) when an enabled quota matches,
// (0, false, nil) when no row matches, and (0, false, err) on a DB error.
// For tiers without an entity_id (system) pass an invalid sql.NullString.
func lookupQuotaLimit(tx *database.Tx, entityType string, entityID sql.NullString) (int64, bool, error) {
	var max int64
	var err error
	if entityID.Valid {
		err = tx.QueryRow(
			`SELECT max_bytes FROM quotas
			  WHERE entity_type = ? AND entity_id = ? AND is_enabled = TRUE`,
			entityType, entityID.String,
		).Scan(&max)
	} else {
		err = tx.QueryRow(
			`SELECT max_bytes FROM quotas
			  WHERE entity_type = ? AND is_enabled = TRUE`,
			entityType,
		).Scan(&max)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return max, true, nil
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
	case "group":
		if entityID.Valid {
			s.db.QueryRow(
				`SELECT COALESCE(SUM(u.file_size), 0)
				   FROM uploads u
				   JOIN group_members gm ON gm.user_id = u.user_id
				  WHERE gm.group_id = CAST(? AS INTEGER) AND u.status = 'completed'`,
				entityID.String,
			).Scan(&used)
		}
	case "department":
		if entityID.Valid {
			s.db.QueryRow(
				`SELECT COALESCE(SUM(u.file_size), 0)
				   FROM uploads u
				   JOIN api_tokens t ON t.id = u.token_id
				  WHERE t.department = ? AND u.status = 'completed'`,
				entityID.String,
			).Scan(&used)
		}
	case "system":
		s.db.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE status = 'completed'`).Scan(&used)
	}
	return used
}

// --- helpers ---

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

func stringSliceToInterfaces(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func appendInterfaces(first interface{}, ss []string) []interface{} {
	out := make([]interface{}, 0, 1+len(ss))
	out = append(out, first)
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

func int64ToString(n int64) string {
	// strconv import would cycle the file's import block in tests; use fmt-free.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
