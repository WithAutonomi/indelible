package services

import (
	"database/sql"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

// AuditLogEntry represents a security/compliance event.
type AuditLogEntry struct {
	ID        int64
	EventType string
	Severity  string
	UserID    sql.NullInt64
	Detail    string
	IPAddress sql.NullString
	UserAgent sql.NullString
	CreatedAt time.Time
}

// SystemLogEntry represents an internal operation event.
type SystemLogEntry struct {
	ID        int64
	Level     string
	Component string
	Message   string
	Detail    sql.NullString
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

// WriteAudit writes an entry to the audit log.
func (s *LogService) WriteAudit(eventType, severity string, userID *int64, detail, ipAddress, userAgent string) error {
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT INTO audit_log (event_type, severity, user_id, detail, ip_address, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		eventType, severity, uid, detail, ipAddress, userAgent,
	)
	return err
}

// WriteSystem writes an entry to the system log.
func (s *LogService) WriteSystem(level, component, message, detail string) error {
	var d sql.NullString
	if detail != "" {
		d = sql.NullString{String: detail, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT INTO system_log (level, component, message, detail) VALUES (?, ?, ?, ?)`,
		level, component, message, d,
	)
	return err
}

// QueryAuditLogs returns audit log entries with optional filters.
func (s *LogService) QueryAuditLogs(eventType string, userID *int64, since, until *time.Time, limit, offset int) ([]*AuditLogEntry, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where, args := buildLogFilter("audit_log", eventType, userID, since, until)

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total)

	rows, err := s.db.Query(
		`SELECT id, event_type, severity, user_id, detail, ip_address, user_agent, created_at FROM audit_log`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*AuditLogEntry
	for rows.Next() {
		e := &AuditLogEntry{}
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.UserID, &e.Detail, &e.IPAddress, &e.UserAgent, &e.CreatedAt); err != nil {
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
		`SELECT id, level, component, message, detail, created_at FROM system_log`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*SystemLogEntry
	for rows.Next() {
		e := &SystemLogEntry{}
		if err := rows.Scan(&e.ID, &e.Level, &e.Component, &e.Message, &e.Detail, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// QueryUserActivity returns audit log entries for user actions (logins, uploads, token ops).
func (s *LogService) QueryUserActivity(userID *int64, since, until *time.Time, limit, offset int) ([]*AuditLogEntry, int64, error) {
	// User logs are audit logs filtered to user-facing event types
	return s.QueryAuditLogs("", userID, since, until, limit, offset)
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

func buildLogFilter(table, eventType string, userID *int64, since, until *time.Time) (string, []any) {
	where := " WHERE 1=1"
	args := []any{}

	if eventType != "" {
		where += " AND event_type = ?"
		args = append(args, eventType)
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
