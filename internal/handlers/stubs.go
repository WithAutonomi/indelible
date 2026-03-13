package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/maidsafe/indelible/internal/config"
)

// stub returns a placeholder handler that responds with 501 Not Implemented.
// Used during scaffolding — each stub will be replaced with a real implementation.
func stub(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "not implemented",
			"endpoint": name,
		})
	}
}

// --- Health ---

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

// Auth, Profile handlers are in auth.go

// Upload handlers are in uploads.go

// Tag handlers are in tags.go

// Collection handlers are in collections.go

// Token handlers are in tokens.go

// --- Notifications ---

func GetNotificationPrefs(db *sql.DB) http.HandlerFunc    { return stub("GET /notifications/preferences") }
func UpdateNotificationPrefs(db *sql.DB) http.HandlerFunc { return stub("PUT /notifications/preferences") }

// Admin user handlers are in admin_users.go

// Admin group handlers are in admin_groups.go

// Admin token handlers are in tokens.go

// Admin wallet handlers are in admin_wallets.go

// --- Admin: Settings ---

func AdminGetSettings(db *sql.DB) http.HandlerFunc       { return stub("GET /admin/settings") }
func AdminUpdateSettings(db *sql.DB) http.HandlerFunc    { return stub("PATCH /admin/settings") }
func AdminExportSettings(db *sql.DB) http.HandlerFunc    { return stub("GET /admin/settings/export") }
func AdminImportSettings(db *sql.DB) http.HandlerFunc    { return stub("POST /admin/settings/import") }

// --- Admin: Webhooks ---

func AdminGetWebhooks(db *sql.DB) http.HandlerFunc       { return stub("GET /admin/webhooks") }
func AdminCreateWebhook(db *sql.DB) http.HandlerFunc     { return stub("POST /admin/webhooks") }
func AdminUpdateWebhook(db *sql.DB) http.HandlerFunc     { return stub("PUT /admin/webhooks/{id}") }
func AdminDeleteWebhook(db *sql.DB) http.HandlerFunc     { return stub("DELETE /admin/webhooks/{id}") }

// --- Admin: OIDC ---

func AdminListOIDCProviders(db *sql.DB) http.HandlerFunc   { return stub("GET /admin/oidc/providers") }
func AdminCreateOIDCProvider(db *sql.DB) http.HandlerFunc  { return stub("POST /admin/oidc/providers") }
func AdminUpdateOIDCProvider(db *sql.DB) http.HandlerFunc  { return stub("PUT /admin/oidc/providers/{id}") }
func AdminDeleteOIDCProvider(db *sql.DB) http.HandlerFunc  { return stub("DELETE /admin/oidc/providers/{id}") }

// Admin analytics handlers are in admin_analytics.go

// --- Admin: Logs ---

func AdminAuditLogs(db *sql.DB) http.HandlerFunc  { return stub("GET /admin/logs/audit") }
func AdminSystemLogs(db *sql.DB) http.HandlerFunc { return stub("GET /admin/logs/system") }
func AdminUserLogs(db *sql.DB) http.HandlerFunc   { return stub("GET /admin/logs/user") }

// --- Admin: Quotas ---

func AdminListQuotas(db *sql.DB) http.HandlerFunc   { return stub("GET /admin/quotas") }
func AdminCreateQuota(db *sql.DB) http.HandlerFunc  { return stub("POST /admin/quotas") }
func AdminUpdateQuota(db *sql.DB) http.HandlerFunc  { return stub("PUT /admin/quotas/{id}") }
func AdminDeleteQuota(db *sql.DB) http.HandlerFunc  { return stub("DELETE /admin/quotas/{id}") }
