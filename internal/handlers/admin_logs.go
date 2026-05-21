package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
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
	RequestID string  `json:"request_id"` // V2-317; "" for worker-emitted rows
	CreatedAt string  `json:"created_at"`
}

type configAuditResponse struct {
	ID         int64   `json:"id"`
	SettingKey string  `json:"setting_key"`
	OldValue   *string `json:"old_value"`
	NewValue   string  `json:"new_value"`
	ChangedBy  *int64  `json:"changed_by"`
	IPAddress  *string `json:"ip_address"`
	UserAgent  *string `json:"user_agent"`
	CreatedAt  string  `json:"created_at"`
}

func toConfigAuditResponse(e *services.ConfigAuditEntry) configAuditResponse {
	r := configAuditResponse{
		ID:         e.ID,
		SettingKey: e.SettingKey,
		NewValue:   e.NewValue,
		CreatedAt:  e.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if e.OldValue.Valid {
		r.OldValue = &e.OldValue.String
	}
	if e.ChangedBy.Valid {
		r.ChangedBy = &e.ChangedBy.Int64
	}
	if e.IPAddress.Valid {
		r.IPAddress = &e.IPAddress.String
	}
	if e.UserAgent.Valid {
		r.UserAgent = &e.UserAgent.String
	}
	return r
}

type systemLogResponse struct {
	ID        int64   `json:"id"`
	Level     string  `json:"level"`
	Component string  `json:"component"`
	Message   string  `json:"message"`
	Detail    *string `json:"detail"`
	RequestID string  `json:"request_id"`
	CreatedAt string  `json:"created_at"`
}

func toAuditLogResponse(e *services.AuditLogEntry) auditLogResponse {
	r := auditLogResponse{
		ID:        e.ID,
		EventType: e.EventType,
		Severity:  e.Severity,
		Detail:    e.Detail,
		RequestID: e.RequestID,
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
		RequestID: e.RequestID,
		CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if e.Detail.Valid {
		r.Detail = &e.Detail.String
	}
	return r
}

// @Summary      Query audit logs
// @Description  Return audit log entries with optional filtering (permanent, never deleted)
// @Tags         Admin: Logs
// @Produce      json
// @Param        event_type query string false "Filter by event type"
// @Param        user_id    query int    false "Filter by user ID"
// @Param        since      query string false "Start date (YYYY-MM-DD)"
// @Param        until      query string false "End date (YYYY-MM-DD)"
// @Param        limit      query int    false "Max results"
// @Param        offset     query int    false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/logs/audit [get]
// @Security     BearerAuth
// AdminAuditLogs returns audit log entries (permanent, never deleted).
func AdminAuditLogs(db *database.DB) http.HandlerFunc {
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

// @Summary      Query system logs
// @Description  Return system log entries with optional filtering (retention-managed)
// @Tags         Admin: Logs
// @Produce      json
// @Param        level     query string false "Filter by log level"
// @Param        component query string false "Filter by component"
// @Param        since     query string false "Start date (YYYY-MM-DD)"
// @Param        until     query string false "End date (YYYY-MM-DD)"
// @Param        limit     query int    false "Max results"
// @Param        offset    query int    false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Summary      Query config-change audit log
// @Description  Return config_audit entries showing old/new value, actor, IP, and UA for every settings change. Permanent (never purged).
// @Tags         Admin: Logs
// @Produce      json
// @Param        setting_key query string false "Filter by setting key"
// @Param        since       query string false "Start date (YYYY-MM-DD)"
// @Param        until       query string false "End date (YYYY-MM-DD)"
// @Param        limit       query int    false "Max results (default 100, max 500)"
// @Param        offset      query int    false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/logs/config [get]
// @Security     BearerAuth
// AdminConfigAuditLogs returns config-change history. Backs FEATURES.md §9's
// "config change audit trail: records old value, new value, who changed, when,
// from where" claim — Update() already writes the rows; this reads them back.
func AdminConfigAuditLogs(db *database.DB) http.HandlerFunc {
	settingsSvc := services.NewSettingsService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		settingKey := r.URL.Query().Get("setting_key")
		_, _, since, until, limit, offset := parseLogFilters(r)
		entries, total, err := settingsSvc.QueryConfigAudit(settingKey, since, until, limit, offset)
		if err != nil {
			jsonError(w, "failed to query config audit", http.StatusInternalServerError)
			return
		}
		resp := make([]configAuditResponse, 0, len(entries))
		for _, e := range entries {
			resp = append(resp, toConfigAuditResponse(e))
		}
		jsonResponse(w, http.StatusOK, map[string]any{
			"entries": resp, "total": total, "limit": limit, "offset": offset,
		})
	}
}

// @Router       /admin/logs/system [get]
// @Security     BearerAuth
// AdminSystemLogs returns system log entries (retention-managed).
func AdminSystemLogs(db *database.DB) http.HandlerFunc {
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

// @Summary      Query user activity logs
// @Description  Return user activity log entries with optional filtering
// @Tags         Admin: Logs
// @Produce      json
// @Param        user_id query int    false "Filter by user ID"
// @Param        since   query string false "Start date (YYYY-MM-DD)"
// @Param        until   query string false "End date (YYYY-MM-DD)"
// @Param        limit   query int    false "Max results"
// @Param        offset  query int    false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/logs/user [get]
// @Security     BearerAuth
// AdminUserLogs returns user activity log entries.
func AdminUserLogs(db *database.DB) http.HandlerFunc {
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
