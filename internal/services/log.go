package services

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

// ExportMaxRows caps streamed log exports. Operators hitting this can
// narrow the date range and call again.
const ExportMaxRows int64 = 1_000_000

// AuditLogEntry represents a security/compliance event.
type AuditLogEntry struct {
	ID        int64
	EventType string
	Severity  string
	UserID    sql.NullInt64
	Detail    string
	IPAddress sql.NullString
	UserAgent sql.NullString
	RequestID string // V2-317: chi X-Request-Id; empty when written outside HTTP path
	CreatedAt time.Time
}

// SystemLogEntry represents an internal operation event.
type SystemLogEntry struct {
	ID        int64
	Level     string
	Component string
	Message   string
	Detail    sql.NullString
	RequestID string
	CreatedAt time.Time
}

// LogService handles log queries and writes.
type LogService struct {
	db *database.DB
}

// NewLogService creates a new LogService.
func NewLogService(db *database.DB) *LogService {
	return &LogService{db: db}
}

// WriteAudit writes an entry to the audit log. requestID should be the chi
// X-Request-Id for the originating request, or "" if written outside an HTTP
// handler (e.g. workers). Callers in handlers should pass
// chimw.GetReqID(r.Context()).
func (s *LogService) WriteAudit(eventType, severity string, userID *int64, detail, ipAddress, userAgent, requestID string) error {
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT INTO audit_log (event_type, severity, user_id, detail, ip_address, user_agent, request_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventType, severity, uid, detail, ipAddress, userAgent, requestID,
	)
	return err
}

// WriteSystem writes an entry to the system log. requestID is "" for worker-
// originated entries.
func (s *LogService) WriteSystem(level, component, message, detail, requestID string) error {
	var d sql.NullString
	if detail != "" {
		d = sql.NullString{String: detail, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT INTO system_log (level, component, message, detail, request_id) VALUES (?, ?, ?, ?, ?)`,
		level, component, message, d, requestID,
	)
	return err
}

// QueryAuditLogs returns audit log entries with optional filters.
// `severity` filters on the audit_log.severity column (info|warn|error); empty matches all.
func (s *LogService) QueryAuditLogs(eventType, severity string, userID *int64, since, until *time.Time, limit, offset int) ([]*AuditLogEntry, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where, args := buildLogFilter("audit_log", eventType, severity, userID, since, until)

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total)

	rows, err := s.db.Query(
		`SELECT id, event_type, severity, user_id, detail, ip_address, user_agent, request_id, created_at FROM audit_log`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*AuditLogEntry
	for rows.Next() {
		e := &AuditLogEntry{}
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.UserID, &e.Detail, &e.IPAddress, &e.UserAgent, &e.RequestID, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// QuerySystemLogs returns system log entries with optional filters.
func (s *LogService) QuerySystemLogs(level, component string, since, until *time.Time, limit, offset int) ([]*SystemLogEntry, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where := " WHERE 1=1"
	args := []any{}

	if level != "" {
		where += " AND level = ?"
		args = append(args, level)
	}
	if component != "" {
		where += " AND component = ?"
		args = append(args, component)
	}
	if since != nil {
		where += " AND created_at >= ?"
		args = append(args, since.Format("2006-01-02T15:04:05"))
	}
	if until != nil {
		where += " AND created_at <= ?"
		args = append(args, until.Format("2006-01-02T15:04:05"))
	}

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM system_log`+where, args...).Scan(&total)

	rows, err := s.db.Query(
		`SELECT id, level, component, message, detail, request_id, created_at FROM system_log`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*SystemLogEntry
	for rows.Next() {
		e := &SystemLogEntry{}
		if err := rows.Scan(&e.ID, &e.Level, &e.Component, &e.Message, &e.Detail, &e.RequestID, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// QueryUserActivity returns audit log entries for user actions (logins, uploads, token ops).
func (s *LogService) QueryUserActivity(severity string, userID *int64, since, until *time.Time, limit, offset int) ([]*AuditLogEntry, int64, error) {
	// User logs are audit logs filtered to user-facing event types
	return s.QueryAuditLogs("", severity, userID, since, until, limit, offset)
}

// QueryConfigAudit returns config_audit entries with optional filters.
// Mirrors QueryAuditLogs in shape — V2-316 surfaces the table that
// SettingsService.Update already populates.
func (s *LogService) QueryConfigAudit(settingKey string, changedBy *int64, since, until *time.Time, limit, offset int) ([]*ConfigAuditEntry, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where := " WHERE 1=1"
	args := []any{}
	if settingKey != "" {
		where += " AND setting_key = ?"
		args = append(args, settingKey)
	}
	if changedBy != nil {
		where += " AND changed_by = ?"
		args = append(args, *changedBy)
	}
	if since != nil {
		where += " AND created_at >= ?"
		args = append(args, since.Format("2006-01-02T15:04:05"))
	}
	if until != nil {
		where += " AND created_at <= ?"
		args = append(args, until.Format("2006-01-02T15:04:05"))
	}

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM config_audit`+where, args...).Scan(&total)

	rows, err := s.db.Query(
		`SELECT id, setting_key, old_value, new_value, changed_by, ip_address, user_agent, created_at FROM config_audit`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*ConfigAuditEntry
	for rows.Next() {
		e := &ConfigAuditEntry{}
		if err := rows.Scan(&e.ID, &e.SettingKey, &e.OldValue, &e.NewValue, &e.ChangedBy, &e.IPAddress, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// CleanupOldLogs deletes system log entries older than the given number of days.
// Audit logs are never deleted.
func (s *LogService) CleanupOldLogs(retentionDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format("2006-01-02 15:04:05")
	result, err := s.db.Exec(
		`DELETE FROM system_log WHERE created_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func buildLogFilter(table, eventType, severity string, userID *int64, since, until *time.Time) (string, []any) {
	where := " WHERE 1=1"
	args := []any{}

	if eventType != "" {
		where += " AND event_type = ?"
		args = append(args, eventType)
	}
	if severity != "" {
		where += " AND severity = ?"
		args = append(args, severity)
	}
	if userID != nil {
		where += " AND user_id = ?"
		args = append(args, *userID)
	}
	if since != nil {
		where += " AND created_at >= ?"
		args = append(args, since.Format("2006-01-02T15:04:05"))
	}
	if until != nil {
		where += " AND created_at <= ?"
		args = append(args, until.Format("2006-01-02T15:04:05"))
	}

	return where, args
}

// ErrExportCapExceeded is returned by Stream* methods when the result set
// exceeds ExportMaxRows. Callers should narrow the date range and retry.
var ErrExportCapExceeded = fmt.Errorf("export exceeded cap of %d rows; narrow the date range and retry", ExportMaxRows)

// StreamAuditLogs walks audit_log under the given filter and invokes emit per row.
// Returns (count, ErrExportCapExceeded) if the cap is hit.
func (s *LogService) StreamAuditLogs(eventType, severity string, userID *int64, since, until *time.Time, emit func(*AuditLogEntry) error) (int64, error) {
	where, args := buildLogFilter("audit_log", eventType, severity, userID, since, until)
	rows, err := s.db.Query(
		`SELECT id, event_type, severity, user_id, detail, ip_address, user_agent, created_at FROM audit_log`+where+` ORDER BY created_at DESC LIMIT ?`,
		append(args, ExportMaxRows+1)...,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var n int64
	for rows.Next() {
		n++
		if n > ExportMaxRows {
			return ExportMaxRows, ErrExportCapExceeded
		}
		e := &AuditLogEntry{}
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.UserID, &e.Detail, &e.IPAddress, &e.UserAgent, &e.CreatedAt); err != nil {
			return n, err
		}
		if err := emit(e); err != nil {
			return n, err
		}
	}
	return n, rows.Err()
}

// StreamSystemLogs walks system_log under the given filter and invokes emit per row.
func (s *LogService) StreamSystemLogs(level, component string, since, until *time.Time, emit func(*SystemLogEntry) error) (int64, error) {
	where := " WHERE 1=1"
	args := []any{}
	if level != "" {
		where += " AND level = ?"
		args = append(args, level)
	}
	if component != "" {
		where += " AND component = ?"
		args = append(args, component)
	}
	if since != nil {
		where += " AND created_at >= ?"
		args = append(args, since.Format("2006-01-02T15:04:05"))
	}
	if until != nil {
		where += " AND created_at <= ?"
		args = append(args, until.Format("2006-01-02T15:04:05"))
	}

	rows, err := s.db.Query(
		`SELECT id, level, component, message, detail, created_at FROM system_log`+where+` ORDER BY created_at DESC LIMIT ?`,
		append(args, ExportMaxRows+1)...,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var n int64
	for rows.Next() {
		n++
		if n > ExportMaxRows {
			return ExportMaxRows, ErrExportCapExceeded
		}
		e := &SystemLogEntry{}
		if err := rows.Scan(&e.ID, &e.Level, &e.Component, &e.Message, &e.Detail, &e.CreatedAt); err != nil {
			return n, err
		}
		if err := emit(e); err != nil {
			return n, err
		}
	}
	return n, rows.Err()
}

// StreamConfigAudit walks config_audit under the given filter and invokes emit per row.
func (s *LogService) StreamConfigAudit(settingKey string, changedBy *int64, since, until *time.Time, emit func(*ConfigAuditEntry) error) (int64, error) {
	where := " WHERE 1=1"
	args := []any{}
	if settingKey != "" {
		where += " AND setting_key = ?"
		args = append(args, settingKey)
	}
	if changedBy != nil {
		where += " AND changed_by = ?"
		args = append(args, *changedBy)
	}
	if since != nil {
		where += " AND created_at >= ?"
		args = append(args, since.Format("2006-01-02T15:04:05"))
	}
	if until != nil {
		where += " AND created_at <= ?"
		args = append(args, until.Format("2006-01-02T15:04:05"))
	}

	rows, err := s.db.Query(
		`SELECT id, setting_key, old_value, new_value, changed_by, ip_address, user_agent, created_at FROM config_audit`+where+` ORDER BY created_at DESC LIMIT ?`,
		append(args, ExportMaxRows+1)...,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var n int64
	for rows.Next() {
		n++
		if n > ExportMaxRows {
			return ExportMaxRows, ErrExportCapExceeded
		}
		e := &ConfigAuditEntry{}
		if err := rows.Scan(&e.ID, &e.SettingKey, &e.OldValue, &e.NewValue, &e.ChangedBy, &e.IPAddress, &e.UserAgent, &e.CreatedAt); err != nil {
			return n, err
		}
		if err := emit(e); err != nil {
			return n, err
		}
	}
	return n, rows.Err()
}
