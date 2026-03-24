package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/WithAutonomi/indelible/internal/config"
)

// --- Health ---

// Health godoc
// @Summary Health check
// @Description Returns system health status including database connectivity
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Router /health [get]
func Health(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check DB connectivity
		dbOK := db.Ping() == nil

		status := http.StatusOK
		if !dbOK {
			status = http.StatusServiceUnavailable
		}

		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"database": dbOK,
			"antd_url": cfg.AntdURL,
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
