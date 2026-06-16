package services

import (
	"database/sql"
	"time"
)

// File-access logging (V2-514).
//
// File *read* telemetry — file_downloaded and file_download_denied, emitted by
// the download handler — lives in its own plain, append-only file_access_log
// table instead of the tamper-evident audit_log hash-chain. WriteFileAccess is
// a bare INSERT: no auditChainMu, no chain-head read, no row_hash. That is the
// whole point — a reader fleet (parent V2-513) serves only the download route,
// so every reader replica writes only this table and never touches the chain,
// which both removes the cross-instance chain-fork hazard and lifts the
// per-write mutex bottleneck off the hot read path.
//
// Security-relevant file *mutations* (file_uploaded, file_deleted, and the
// upload/delete denials) deliberately stay in audit_log so they remain
// tamper-evident. The read/write column shape matches audit_log so the same
// AuditLogEntry struct, response mapper, and query helpers are reused.

// WriteFileAccess appends a file-access event. Unlike WriteAudit it does no
// hash-chaining and takes no lock, so concurrent writers across instances are
// safe and uncontended. requestID is the chi X-Request-Id, or "" outside HTTP.
func (s *LogService) WriteFileAccess(eventType, severity string, userID *int64, detail, ipAddress, userAgent, requestID string) error {
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT INTO file_access_log (event_type, severity, user_id, detail, ip_address, user_agent, request_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventType, severity, uid, detail, ipAddress, userAgent, requestID,
	)
	return err
}

// QueryFileAccessLogs returns file-access entries with optional filters.
// Mirrors QueryAuditLogs (same columns, same filter builder).
func (s *LogService) QueryFileAccessLogs(eventType, severity string, userID *int64, since, until *time.Time, limit, offset int) ([]*AuditLogEntry, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where, args := buildLogFilter("file_access_log", eventType, severity, userID, since, until)

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM file_access_log`+where, args...).Scan(&total)

	rows, err := s.db.Query(
		`SELECT id, event_type, severity, user_id, detail, ip_address, user_agent, request_id, created_at FROM file_access_log`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
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

// StreamFileAccessLogs walks file_access_log under the given filter and invokes
// emit per row. Returns (count, ErrExportCapExceeded) if the cap is hit.
// Mirrors StreamAuditLogs.
func (s *LogService) StreamFileAccessLogs(eventType, severity string, userID *int64, since, until *time.Time, emit func(*AuditLogEntry) error) (int64, error) {
	where, args := buildLogFilter("file_access_log", eventType, severity, userID, since, until)
	rows, err := s.db.Query(
		`SELECT id, event_type, severity, user_id, detail, ip_address, user_agent, created_at FROM file_access_log`+where+` ORDER BY created_at DESC LIMIT ?`,
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

// QueryFileAccessStats returns aggregate statistics over file_access_log.
// Mirrors QueryAuditStats.
func (s *LogService) QueryFileAccessStats() (*LogStats, error) {
	st := &LogStats{}

	if err := s.db.QueryRow(
		`SELECT COUNT(*), MIN(created_at), MAX(created_at) FROM file_access_log`,
	).Scan(&st.TotalEntries, &nullTime{&st.Earliest}, &nullTime{&st.Latest}); err != nil {
		return nil, err
	}

	st.DiskUsageBytes = s.tableSize("file_access_log")

	rows, err := s.db.Query(`SELECT severity, COUNT(*) FROM file_access_log GROUP BY severity`)
	if err != nil {
		return nil, err
	}
	st.BySeverity, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT event_type, COUNT(*) c FROM file_access_log GROUP BY event_type ORDER BY c DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	st.ByEventType, err = scanGroupCounts(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.Query(
		`SELECT `+s.dayExpr()+` AS d, COUNT(*) FROM file_access_log WHERE created_at >= ? GROUP BY d`,
		thirtyDaysAgo(),
	)
	if err != nil {
		return nil, err
	}
	st.ByDay, err = scanByDay(rows)
	rows.Close()
	return st, err
}
