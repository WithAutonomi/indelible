package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/services"
)

// --- Health ---

// Health godoc
// @Summary Health check
// @Description Returns system health status including database, antd, and queue
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Router /health [get]
func Health(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check DB connectivity
		dbOK := db.Ping() == nil

		// Check antd reachability (2s timeout)
		antdOK := false
		if cfg.AntdURL != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", cfg.AntdURL+"/v1/node/version", nil)
			if err == nil {
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					antdOK = resp.StatusCode < 500
					resp.Body.Close()
				}
			}
		}

		// Queue depth
		counts, _ := uploadSvc.CountByStatus()
		queued := counts["queued"]
		processing := counts["processing"]

		// DB is the hard requirement; antd is informational
		status := http.StatusOK
		statusText := "healthy"
		if !dbOK {
			status = http.StatusServiceUnavailable
			statusText = "unhealthy"
		} else if !antdOK {
			statusText = "degraded"
		}

		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{
			"status":     statusText,
			"database":   dbOK,
			"antd":       antdOK,
			"antd_url":   cfg.AntdURL,
			"queued":     queued,
			"processing": processing,
		})
	}
}

// Handler locations:
// - Auth, Profile: auth.go
// - Uploads: uploads.go
// - Tags: tags.go
// - Collections: collections.go
// - Tokens: tokens.go
// - Notifications: notifications.go
// - Admin Users: admin_users.go
// - Admin Groups: admin_groups.go
// - Admin Wallets: admin_wallets.go
// - Admin Settings: admin_settings.go
// - Admin Webhooks: admin_webhooks.go
// - Admin OIDC: admin_oidc.go
// - Admin Analytics: admin_analytics.go
// - Admin Logs: admin_logs.go
// - Admin Quotas: admin_quotas.go
// - Admin SCIM: admin_scim.go
// - SCIM Handlers: scim.go
