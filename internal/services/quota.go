package services

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrQuotaNotFound  = errors.New("quota not found")
	ErrQuotaExceeded  = errors.New("quota exceeded")
	ErrQuotaDuplicate = errors.New("quota already exists for this entity")
	// ErrQuotaEntityRequired is returned when a user/group/department quota is
	// created without an entity_id. Such a row would match no entity and
	// enforce nothing (only a system quota may omit entity_id).
	ErrQuotaEntityRequired = errors.New("entity_id is required for user, group, and department quotas")
	// ErrQuotaEntityNotFound is returned when the entity_id of a user/group
	// quota doesn't reference an existing user/group — a typo'd id would
	// otherwise create a silently inert quota.
	ErrQuotaEntityNotFound = errors.New("no user or group exists with that entity_id")
	// ErrQuotaInvalidEntityType is returned for an unrecognised entity_type.
	ErrQuotaInvalidEntityType = errors.New("entity_type must be one of: system, user, group, department")
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

// Create adds a new quota. The entity is validated first: only a system quota
// may omit entity_id, and user/group ids must reference an existing row, so a
// blank or typo'd id can't create a quota that silently enforces nothing.
func (s *QuotaService) Create(entityType, entityID string, maxBytes int64) (*Quota, error) {
	entityType, entityID, err := s.validateEntity(entityType, entityID)
	if err != nil {
		return nil, err
	}

	var eID sql.NullString
	if entityID != "" {
		eID = sql.NullString{String: entityID, Valid: true}
	}

	var id int64
	err = s.db.QueryRow(
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

// validateEntity normalises and validates an (entityType, entityID) pair for a
// new quota. It returns the canonical pair to store, or a typed error:
//   - system: entity_id is cleared (a system quota applies to everyone).
//   - user/group/department: entity_id is required (ErrQuotaEntityRequired).
//   - user/group: entity_id must reference an existing row (ErrQuotaEntityNotFound).
//
// Department ids are free-text labels (matched against api_tokens.department)
// and may legitimately predate any token, so their existence is not checked.
func (s *QuotaService) validateEntity(entityType, entityID string) (string, string, error) {
	switch entityType {
	case "system":
		return entityType, "", nil
	case "user", "group", "department":
		if entityID == "" {
			return "", "", ErrQuotaEntityRequired
		}
	default:
		return "", "", ErrQuotaInvalidEntityType
	}

	switch entityType {
	case "user":
		if !s.entityExists(`SELECT 1 FROM users WHERE id = ?`, entityID) {
			return "", "", ErrQuotaEntityNotFound
		}
	case "group":
		if !s.entityExists(`SELECT 1 FROM groups WHERE id = ?`, entityID) {
			return "", "", ErrQuotaEntityNotFound
		}
	}
	return entityType, entityID, nil
}

// entityExists reports whether query (a `SELECT 1 ... WHERE id = ?`) matches a
// row for the given id. A non-numeric id can't reference a user/group, so it's
// rejected before hitting the DB (CAST behaviour differs across dialects).
func (s *QuotaService) entityExists(query, id string) bool {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return false
	}
	var one int
	err := s.db.QueryRow(query, id).Scan(&one)
	return err == nil
}

// Departments returns the distinct, non-empty department labels currently in
// use across API tokens, sorted — used to populate the quota dialog's
// department picker with suggestions (free-text entry is still allowed).
func (s *QuotaService) Departments() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT department FROM api_tokens
		  WHERE department IS NOT NULL AND department != ''
		  ORDER BY department`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	depts := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		depts = append(depts, d)
	}
	return depts, rows.Err()
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

// SystemQuota returns the enabled system-wide quota with its current usage, or
// nil if none is configured (or it exists but is disabled). Used by the System
// page to show used-vs-quota alongside raw disk usage.
func (s *QuotaService) SystemQuota() (*Quota, error) {
	q := &Quota{}
	err := s.db.QueryRow(
		`SELECT id, entity_type, entity_id, max_bytes, is_enabled, created_at, updated_at
		   FROM quotas WHERE entity_type = 'system' AND is_enabled = TRUE`,
	).Scan(&q.ID, &q.EntityType, &q.EntityID, &q.MaxBytes, &q.IsEnabled, &q.CreatedAt, &q.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q.UsedBytes = s.calcUsage(q.EntityType, q.EntityID)
	return q, nil
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
	// "already_stored" counts as stored usage too — the content is on the
	// network and belongs to the user, so a dedup re-upload must not let them
	// slip past a quota (V2-399).
	quotaCompletedStatuses = []string{"completed", "already_stored"}
	quotaInFlightStatuses  = []string{"completed", "already_stored", "processing", "queued"}
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
				`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE user_id = ? AND status IN ('completed', 'already_stored')`, entityID.String,
			).Scan(&used)
		}
	case "group":
		if entityID.Valid {
			s.db.QueryRow(
				`SELECT COALESCE(SUM(u.file_size), 0)
				   FROM uploads u
				   JOIN group_members gm ON gm.user_id = u.user_id
				  WHERE gm.group_id = CAST(? AS INTEGER) AND u.status IN ('completed', 'already_stored')`,
				entityID.String,
			).Scan(&used)
		}
	case "department":
		if entityID.Valid {
			s.db.QueryRow(
				`SELECT COALESCE(SUM(u.file_size), 0)
				   FROM uploads u
				   JOIN api_tokens t ON t.id = u.token_id
				  WHERE t.department = ? AND u.status IN ('completed', 'already_stored')`,
				entityID.String,
			).Scan(&used)
		}
	case "system":
		s.db.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM uploads WHERE status IN ('completed', 'already_stored')`).Scan(&used)
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
