package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/maidsafe/indelible/internal/services"
)

func parseLogFilters(r *http.Request) (eventType string, userID *int64, since, until *time.Time, limit, offset int) {
	eventType = r.URL.Query().Get("event_type")
	if uidStr := r.URL.Query().Get("user_id"); uidStr != "" {
		if uid, err := strconv.ParseInt(uidStr, 10, 64); err == nil {
			userID = &uid
		}
	}
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			since = &t
		}
	}
	if u := r.URL.Query().Get("until"); u != "" {
		if t, err := time.Parse("2006-01-02", u); err == nil {
			until = &t
		}
	}
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	return
}

type auditLogResponse struct {
	ID        int64   `json:"id"`
	EventType string  `json:"event_type"`
	Severity  string  `json:"severity"`
	UserID    *int64  `json:"user_id"`
	Detail    string  `json:"detail"`
	IPAddress *string `json:"ip_address"`
	UserAgent *string `json:"user_agent"`
	CreatedAt string  `json:"created_at"`
}

type systemLogResponse struct {
	ID        int64   `json:"id"`
	Level     string  `json:"level"`
	Component string  `json:"component"`
	Message   string  `json:"message"`
	Detail    *string `json:"detail"`
	CreatedAt string  `json:"created_at"`
}

func toAuditLogResponse(e *services.AuditLogEntry) auditLogResponse {
	r := auditLogResponse{
		ID:        e.ID,
		EventType: e.EventType,
		Severity:  e.Severity,
		Detail:    e.Detail,
		CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if e.UserID.Valid {
		r.UserID = &e.UserID.Int64
	}
	if e.IPAddress.Valid {
		r.IPAddress = &e.IPAddress.String
	}
	if e.UserAgent.Valid {
		r.UserAgent = &e.UserAgent.String
	}
	return r
}

func toSystemLogResponse(e *services.SystemLogEntry) systemLogResponse {
	r := systemLogResponse{
		ID:        e.ID,
		Level:     e.Level,
		Component: e.Component,
		Message:   e.Message,
		CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if e.Detail.Valid {
		r.Detail = &e.Detail.String
	}
	return r
}

// AdminAuditLogs returns audit log entries (permanent, never deleted).
func AdminAuditLogs(db *sql.DB) http.HandlerFunc {
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		eventType, userID, since, until, limit, offset := parseLogFilters(r)

		entries, total, err := logSvc.QueryAuditLogs(eventType, userID, since, until, limit, offset)
		if err != nil {
			jsonError(w, "failed to query audit logs", http.StatusInternalServerError)
			return
		}

		resp := make([]auditLogResponse, 0, len(entries))
		for _, e := range entries {
			resp = append(resp, toAuditLogResponse(e))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"entries": resp,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	}
}

// AdminSystemLogs returns system log entries (retention-managed).
func AdminSystemLogs(db *sql.DB) http.HandlerFunc {
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		level := r.URL.Query().Get("level")
		component := r.URL.Query().Get("component")
		_, _, since, until, limit, offset := parseLogFilters(r)

		entries, total, err := logSvc.QuerySystemLogs(level, component, since, until, limit, offset)
		if err != nil {
			jsonError(w, "failed to query system logs", http.StatusInternalServerError)
			return
		}

		resp := make([]systemLogResponse, 0, len(entries))
		for _, e := range entries {
			resp = append(resp, toSystemLogResponse(e))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"entries": resp,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	}
}

// AdminUserLogs returns user activity log entries.
func AdminUserLogs(db *sql.DB) http.HandlerFunc {
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		_, userID, since, until, limit, offset := parseLogFilters(r)

		entries, total, err := logSvc.QueryUserActivity(userID, since, until, limit, offset)
		if err != nil {
			jsonError(w, "failed to query user logs", http.StatusInternalServerError)
			return
		}

		resp := make([]auditLogResponse, 0, len(entries))
		for _, e := range entries {
			resp = append(resp, toAuditLogResponse(e))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"entries": resp,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	}
}
