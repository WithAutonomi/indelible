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

// --- Uploads ---

func CreateUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc  { return stub("POST /uploads") }
func ListUploads(db *sql.DB) http.HandlerFunc                       { return stub("GET /uploads") }
func GetUpload(db *sql.DB) http.HandlerFunc                         { return stub("GET /uploads/{id}") }
func QuoteUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc   { return stub("POST /uploads/quote") }
func DownloadUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	return stub("GET /uploads/{id}/download")
}

// --- Tags ---

func UpdateTags(db *sql.DB) http.HandlerFunc  { return stub("PUT /uploads/{id}/tags") }
func SearchByTags(db *sql.DB) http.HandlerFunc { return stub("GET /tags/search") }

// --- Collections ---

func CreateCollection(db *sql.DB) http.HandlerFunc                    { return stub("POST /collections") }
func ListCollections(db *sql.DB) http.HandlerFunc                     { return stub("GET /collections") }
func GetCollection(db *sql.DB) http.HandlerFunc                       { return stub("GET /collections/{id}") }
func UpdateCollection(db *sql.DB) http.HandlerFunc                    { return stub("PUT /collections/{id}") }
func DeleteCollection(db *sql.DB) http.HandlerFunc                    { return stub("DELETE /collections/{id}") }
func AddToCollection(db *sql.DB) http.HandlerFunc                     { return stub("POST /collections/{id}/files") }
func RemoveFromCollection(db *sql.DB) http.HandlerFunc                { return stub("DELETE /collections/{id}/files/{uploadId}") }

// --- Tokens ---

func CreateToken(db *sql.DB, cfg *config.Config) http.HandlerFunc { return stub("POST /tokens") }
func ListTokens(db *sql.DB) http.HandlerFunc                      { return stub("GET /tokens") }
func RevokeToken(db *sql.DB) http.HandlerFunc                     { return stub("DELETE /tokens/{id}") }

// --- Notifications ---

func GetNotificationPrefs(db *sql.DB) http.HandlerFunc    { return stub("GET /notifications/preferences") }
func UpdateNotificationPrefs(db *sql.DB) http.HandlerFunc { return stub("PUT /notifications/preferences") }

// Admin user handlers are in admin_users.go

// Admin group handlers are in admin_groups.go

// --- Admin: Tokens ---

func AdminListAllTokens(db *sql.DB) http.HandlerFunc     { return stub("GET /admin/tokens") }
func AdminBulkRevokeTokens(db *sql.DB) http.HandlerFunc  { return stub("DELETE /admin/tokens/bulk") }

// --- Admin: Wallets ---

func AdminListWallets(db *sql.DB) http.HandlerFunc       { return stub("GET /admin/wallets") }
func AdminCreateWallet(db *sql.DB) http.HandlerFunc      { return stub("POST /admin/wallets") }
func AdminSetDefaultWallet(db *sql.DB) http.HandlerFunc  { return stub("PUT /admin/wallets/{id}/default") }

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

// --- Admin: Analytics ---

func AdminUploadAnalytics(db *sql.DB) http.HandlerFunc { return stub("GET /admin/analytics/uploads") }
func AdminTokenAnalytics(db *sql.DB) http.HandlerFunc  { return stub("GET /admin/analytics/tokens") }
func AdminCostAnalytics(db *sql.DB) http.HandlerFunc   { return stub("GET /admin/analytics/costs") }

// --- Admin: Logs ---

func AdminAuditLogs(db *sql.DB) http.HandlerFunc  { return stub("GET /admin/logs/audit") }
func AdminSystemLogs(db *sql.DB) http.HandlerFunc { return stub("GET /admin/logs/system") }
func AdminUserLogs(db *sql.DB) http.HandlerFunc   { return stub("GET /admin/logs/user") }

// --- Admin: Quotas ---

func AdminListQuotas(db *sql.DB) http.HandlerFunc   { return stub("GET /admin/quotas") }
func AdminCreateQuota(db *sql.DB) http.HandlerFunc  { return stub("POST /admin/quotas") }
func AdminUpdateQuota(db *sql.DB) http.HandlerFunc  { return stub("PUT /admin/quotas/{id}") }
func AdminDeleteQuota(db *sql.DB) http.HandlerFunc  { return stub("DELETE /admin/quotas/{id}") }
