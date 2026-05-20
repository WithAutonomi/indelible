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
// `severity` filters on the audit_log.severity column (info|warn|error); empty matches all.
func (s *LogService) QueryAuditLogs(eventType, severity string, userID *int64, since, until *time.Time, limit, offset int) ([]*AuditLogEntry, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where, args := buildLogFilter("audit_log", eventType, severity, userID, since, until)

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

// DayCount is one bucket in a `by_day` breakdown.
type DayCount struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Count int64  `json:"count"`
}

// LogStats is the shared shape for log stats responses. Fields specific to a
// log type live in dedicated maps; unused maps for a given type are omitted
// from the JSON via empty-map elision.
type LogStats struct {
	TotalEntries int64           `json:"total_entries"`
	Earliest     *time.Time      `json:"earliest,omitempty"`
	Latest       *time.Time      `json:"latest,omitempty"`
	// DiskUsageBytes is 0 when the dialect doesn't expose a per-table size
	// (SQLite has no straightforward way to break a whole-DB file down per
	// table). On Postgres it's pg_total_relation_size for the underlying table.
	DiskUsageBytes int64 `json:"disk_usage_bytes"`

	// One of these will be populated depending on the log type.
	BySeverity   map[string]int64 `json:"by_severity,omitempty"`   // audit_log
	ByEventType  map[string]int64 `json:"by_event_type,omitempty"` // audit_log (top 10)
	ByLevel      map[string]int64 `json:"by_level,omitempty"`      // system_log
	ByComponent  map[string]int64 `json:"by_component,omitempty"`  // system_log (top 10)
	BySettingKey map[string]int64 `json:"by_setting_key,omitempty"` // config_audit (top 10)

	ByDay []DayCount `json:"by_day"` // last 30 days, ascending
}

// dayExpr returns the dialect-specific expression that buckets a created_at
// timestamp to a YYYY-MM-DD string for GROUP BY.
func (s *LogService) dayExpr() string {
	if s.db.Driver() == "postgres" {
		return "to_char(created_at, 'YYYY-MM-DD')"
	}
	return "date(created_at)"
}

// tableSize returns the on-disk size of the named table in bytes, or 0 if
// the dialect doesn't expose it.
func (s *LogService) tableSize(table string) int64 {
	if s.db.Driver() != "postgres" {
		return 0
	}
	var n int64
	// Use parameter binding so the rebinder still handles ? → $1 on Postgres.
	if err := s.db.QueryRow(`SELECT pg_total_relation_size(?)`, table).Scan(&n); err != nil {
		return 0
	}
	return n
}

// scanByDay reads {date, count} rows and pads missing days to a full 30-day
// window ending today. Always returns 30 entries ordered ascending.
func scanByDay(rows *sql.Rows) ([]DayCount, error) {
	got := map[string]int64{}
	for rows.Next() {
		var d string
		var c int64
		if err := rows.Scan(&d, &c); err != nil {
			return nil, err
		}
		got[d] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DayCount, 0, 30)
	today := time.Now().UTC()
	for i := 29; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, DayCount{Date: d, Count: got[d]})
	}
	return out, nil
}

// thirtyDaysAgo returns the cutoff for the by_day window in DB-comparable
// string form (matches the format used elsewhere in this file).
func thirtyDaysAgo() string {
	return time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02T15:04:05")
}

// scanGroupCounts reads {key, count} rows into a map.
func scanGroupCounts(rows *sql.Rows) (map[string]int64, error) {
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var c int64
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, rows.Err()
}

// QueryAuditStats returns aggregate statistics over audit_log.
func (s *LogService) QueryAuditStats() (*LogStats, error) {
	st := &LogStats{}

	if err := s.db.QueryRow(
		`SELECT COUNT(*), MIN(created_at), MAX(created_at) FROM audit_log`,
	).Scan(&st.TotalEntries, &nullTime{&st.Earliest}, &nullTime{&st.Latest}); err != nil {
		return nil, err
	}

	st.DiskUsageBytes = s.tableSize("audit_log")

	rows, err := s.db.Query(`SELECT severity, COUNT(*) FROM audit_log GROUP BY severity`)
	if err != nil {
		return nil, err
	}
	st.BySeverity, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT event_type, COUNT(*) c FROM audit_log GROUP BY event_type ORDER BY c DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	st.ByEventType, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(
		`SELECT `+s.dayExpr()+` AS d, COUNT(*) FROM audit_log WHERE created_at >= ? GROUP BY d`,
		thirtyDaysAgo(),
	)
	if err != nil {
		return nil, err
	}
	st.ByDay, err = scanByDay(rows)
	rows.Close()
	return st, err
}

// QuerySystemStats returns aggregate statistics over system_log.
func (s *LogService) QuerySystemStats() (*LogStats, error) {
	st := &LogStats{}

	if err := s.db.QueryRow(
		`SELECT COUNT(*), MIN(created_at), MAX(created_at) FROM system_log`,
	).Scan(&st.TotalEntries, &nullTime{&st.Earliest}, &nullTime{&st.Latest}); err != nil {
		return nil, err
	}

	st.DiskUsageBytes = s.tableSize("system_log")

	rows, err := s.db.Query(`SELECT level, COUNT(*) FROM system_log GROUP BY level`)
	if err != nil {
		return nil, err
	}
	st.ByLevel, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT component, COUNT(*) c FROM system_log GROUP BY component ORDER BY c DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	st.ByComponent, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(
		`SELECT `+s.dayExpr()+` AS d, COUNT(*) FROM system_log WHERE created_at >= ? GROUP BY d`,
		thirtyDaysAgo(),
	)
	if err != nil {
		return nil, err
	}
	st.ByDay, err = scanByDay(rows)
	rows.Close()
	return st, err
}

// QueryConfigAuditStats returns aggregate statistics over config_audit.
func (s *LogService) QueryConfigAuditStats() (*LogStats, error) {
	st := &LogStats{}

	if err := s.db.QueryRow(
		`SELECT COUNT(*), MIN(created_at), MAX(created_at) FROM config_audit`,
	).Scan(&st.TotalEntries, &nullTime{&st.Earliest}, &nullTime{&st.Latest}); err != nil {
		return nil, err
	}

	st.DiskUsageBytes = s.tableSize("config_audit")

	rows, err := s.db.Query(`SELECT setting_key, COUNT(*) c FROM config_audit GROUP BY setting_key ORDER BY c DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	st.BySettingKey, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(
		`SELECT `+s.dayExpr()+` AS d, COUNT(*) FROM config_audit WHERE created_at >= ? GROUP BY d`,
		thirtyDaysAgo(),
	)
	if err != nil {
		return nil, err
	}
	st.ByDay, err = scanByDay(rows)
	rows.Close()
	return st, err
}

// nullTime adapts a **time.Time target to database/sql's Scanner so MIN/MAX
// over an empty table can land as NULL → nil without erroring.
type nullTime struct{ dst **time.Time }

func (n *nullTime) Scan(v any) error {
	if v == nil {
		*n.dst = nil
		return nil
	}
	switch t := v.(type) {
	case time.Time:
		*n.dst = &t
		return nil
	case string:
		// SQLite returns ISO strings; parse with the formats the rest of the file uses.
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano} {
			if parsed, err := time.Parse(layout, t); err == nil {
				*n.dst = &parsed
				return nil
			}
		}
		return fmt.Errorf("nullTime: unrecognized string %q", t)
	case []byte:
		return n.Scan(string(t))
	default:
		return fmt.Errorf("nullTime: unsupported scan type %T", v)
	}
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
