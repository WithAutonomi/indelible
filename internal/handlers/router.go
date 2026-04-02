package handlers

import (
	"database/sql"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/web"
)

// NewRouter builds the application router with all routes registered.
func NewRouter(cfg *config.Config, db *sql.DB) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(propagateRequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Idempotency-Key"},
		ExposedHeaders:   []string{"Link", "X-Request-Id", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After", "X-Idempotent-Replayed"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check (no auth)
	r.Get("/health", Health(db, cfg))

	// Swagger API docs
	r.Get("/api/docs/*", httpSwagger.WrapHandler)

	// API v2 routes
	r.Route("/api/v2", func(r chi.Router) {
		// Maintenance mode (exempt: health, admin routes)
		r.Use(middleware.MaintenanceMode(db))

		// Public auth routes (with rate limiting on login)
		r.Group(func(r chi.Router) {
			loginRL := middleware.RateLimit(5, 60*time.Second, cfg.TrustedProxies)
			resetRL := middleware.RateLimit(3, 60*time.Second, cfg.TrustedProxies)

			r.With(loginRL).Post("/auth/login", Login(db, cfg))
			r.Post("/auth/register", Register(db, cfg))
			r.Post("/auth/logout", Logout())
			r.With(resetRL).Post("/auth/forgot-password", ForgotPassword(db, cfg))
			r.With(resetRL).Post("/auth/reset-password", ResetPassword(db, cfg))
			r.Get("/auth/verify-email", VerifyEmail(db))
		})

		// Authenticated routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(db, cfg))

			// System status (available to all authenticated users)
			r.Get("/system/wallet-status", WalletStatus(db, cfg))
			r.Get("/system/queue-status", QueueStatus(db))

			// Profile
			r.Get("/me", GetProfile(db))
			r.Put("/me", UpdateProfile(db))
			r.Post("/me/resend-verification", ResendVerification(db, cfg))
			r.Put("/me/password", ChangePassword(db, cfg))

			// Uploads (S7: rate limited to 60/min per user)
			uploadRL := middleware.RateLimitByUser(60, 60*time.Second)
			r.With(uploadRL, middleware.Idempotency(db)).Post("/uploads", CreateUpload(db, cfg))
			r.Get("/uploads", ListUploads(db))
			r.Get("/uploads/{id}", GetUpload(db))
			r.Post("/uploads/quote", QuoteUpload(db, cfg))
			r.Get("/uploads/{id}/download", DownloadUpload(db, cfg))
			r.Post("/uploads/{id}/cancel", CancelUpload(db))
			r.Post("/uploads/{id}/retry", RetryUpload(db))
			r.Post("/uploads/{id}/force-retry", ForceRetryUpload(db))
			r.Delete("/uploads/{id}", DeleteUpload(db))
			r.Get("/uploads/{id}/collections", UploadCollections(db))

			// Tags
			r.Get("/uploads/{id}/tags", GetTags(db))
			r.Put("/uploads/{id}/tags", UpdateTags(db))
			r.Get("/tags/keys", ListTagKeys(db))
			r.Get("/tags/values", ListTagValues(db))
			r.Get("/tags/search", SearchByTags(db))
			r.Post("/tags/bulk", BulkTagUploads(db))
			r.Get("/tags/facets", TagFacets(db))

			// Collections
			r.Post("/collections", CreateCollection(db))
			r.Get("/collections", ListCollections(db))
			r.Get("/collections/{id}", GetCollection(db))
			r.Put("/collections/{id}", UpdateCollection(db))
			r.Delete("/collections/{id}", DeleteCollection(db))
			r.Post("/collections/{id}/files", AddToCollection(db))
			r.Delete("/collections/{id}/files/{uploadId}", RemoveFromCollection(db))
			r.Get("/collections/{id}/tags", GetCollectionTags(db))
			r.Put("/collections/{id}/tags", UpdateCollectionTags(db))

			// API tokens (own)
			r.Post("/tokens", CreateToken(db, cfg))
			r.Get("/tokens", ListTokens(db))
			r.Delete("/tokens/{id}", RevokeToken(db))

			// Notification preferences
			r.Get("/notifications/preferences", GetNotificationPrefs(db))
			r.Put("/notifications/preferences", UpdateNotificationPrefs(db))
		})

		// Admin routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(db, cfg))
			r.Use(middleware.RequireAdmin(db))

			// Tag rules (admin)
			r.Get("/admin/tag-rules", ListTagRules(db))
			r.Post("/admin/tag-rules", CreateTagRule(db))
			r.Put("/admin/tag-rules/{id}", UpdateTagRule(db))
			r.Delete("/admin/tag-rules/{id}", DeleteTagRule(db))

			// User management
			r.Post("/admin/users", AdminCreateUser(db))
			r.Get("/admin/users", AdminListUsers(db))
			r.Get("/admin/users/{id}", AdminGetUser(db))
			r.Put("/admin/users/{id}", AdminUpdateUser(db))
			r.Delete("/admin/users/{id}", AdminDeleteUser(db))
			r.Post("/admin/users/service-accounts", AdminCreateServiceAccount(db))
			r.Put("/admin/users/{id}/permissions", AdminSetPermissions(db))

			// Group management
			r.Get("/admin/groups", AdminListGroups(db))
			r.Post("/admin/groups", AdminCreateGroup(db))
			r.Put("/admin/groups/{id}", AdminUpdateGroup(db))
			r.Delete("/admin/groups/{id}", AdminDeleteGroup(db))
			r.Post("/admin/groups/{id}/members", AdminAddGroupMember(db))
			r.Delete("/admin/groups/{id}/members/{userId}", AdminRemoveGroupMember(db))

			// Token management (all tokens)
			r.Get("/admin/tokens", AdminListAllTokens(db))
			r.Delete("/admin/tokens/bulk", AdminBulkRevokeTokens(db))

			// Wallet management
			r.Get("/admin/wallets", AdminListWallets(db, cfg))
			r.Post("/admin/wallets", AdminCreateWallet(db, cfg))
			r.Put("/admin/wallets/{id}/default", AdminSetDefaultWallet(db, cfg))
			r.Delete("/admin/wallets/{id}", AdminDeleteWallet(db, cfg))
			r.Post("/admin/wallets/{id}/balance", AdminRefreshWalletBalance(db, cfg))

			// System settings
			r.Get("/admin/settings", AdminGetSettings(db))
			r.Patch("/admin/settings", AdminUpdateSettings(db))
			r.Get("/admin/settings/export", AdminExportSettings(db))
			r.Post("/admin/settings/import", AdminImportSettings(db))

			// Webhooks
			r.Get("/admin/webhooks", AdminGetWebhooks(db))
			r.Post("/admin/webhooks", AdminCreateWebhook(db))
			r.Put("/admin/webhooks/{id}", AdminUpdateWebhook(db))
			r.Patch("/admin/webhooks/{id}", AdminUpdateWebhook(db))
			r.Delete("/admin/webhooks/{id}", AdminDeleteWebhook(db))
			r.Post("/admin/webhooks/{id}/test", AdminTestWebhook(db))
			r.Post("/admin/webhooks/{id}/rotate-secret", AdminRotateWebhookSecret(db))
			r.Get("/admin/webhooks/{id}/deliveries", AdminGetWebhookDeliveries(db))

			// SCIM token management
			r.Post("/admin/scim/tokens", AdminCreateScimToken(db))
			r.Get("/admin/scim/tokens", AdminListScimTokens(db))
			r.Delete("/admin/scim/tokens/{id}", AdminRevokeScimToken(db))

			// OIDC providers
			r.Get("/admin/oidc/providers", AdminListOIDCProviders(db, cfg))
			r.Post("/admin/oidc/providers", AdminCreateOIDCProvider(db, cfg))
			r.Put("/admin/oidc/providers/{id}", AdminUpdateOIDCProvider(db, cfg))
			r.Delete("/admin/oidc/providers/{id}", AdminDeleteOIDCProvider(db, cfg))

			// Analytics
			r.Get("/admin/analytics/uploads", AdminUploadAnalytics(db))
			r.Get("/admin/analytics/tokens", AdminTokenAnalytics(db))
			r.Get("/admin/analytics/costs", AdminCostAnalytics(db))

			// Logs
			r.Get("/admin/logs/audit", AdminAuditLogs(db))
			r.Get("/admin/logs/system", AdminSystemLogs(db))
			r.Get("/admin/logs/user", AdminUserLogs(db))

			// Quotas
			r.Get("/admin/quotas", AdminListQuotas(db))
			r.Post("/admin/quotas", AdminCreateQuota(db))
			r.Put("/admin/quotas/{id}", AdminUpdateQuota(db))
			r.Delete("/admin/quotas/{id}", AdminDeleteQuota(db))
		})
	})

	// SCIM 2.0 provisioning endpoint
	scimServer, err := NewSCIMServer(db)
	if err == nil {
		r.Route("/scim/v2", func(r chi.Router) {
			r.Use(middleware.SCIMAuth(db))
			r.Mount("/", scimServer)
		})
	}

	// Serve embedded Vue SPA for all other routes
	spa, err := fs.Sub(web.StaticFS, "dist")
	if err != nil {
		panic("embedded frontend not found: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(spa))
	r.Handle("/*", spaHandler(fileServer))

	return r
}

// propagateRequestID copies the Chi-generated request ID into the response header.
func propagateRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqID := chimw.GetReqID(r.Context()); reqID != "" {
			w.Header().Set("X-Request-Id", reqID)
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler serves static files, falling back to index.html for SPA routing.
// Vue Router uses client-side routes (e.g. /login, /admin/users) that don't
// correspond to real files. When the file server would return 404, we serve
// index.html instead and let the Vue app handle routing.
func spaHandler(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Capture the response to detect 404s
		rec := &spaResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// If the file server returned 404, serve index.html instead
		if rec.status == http.StatusNotFound {
			r.URL.Path = "/"
			rec.reset()
			next.ServeHTTP(w, r)
		}
	}
}

// spaResponseWriter wraps http.ResponseWriter to capture the status code
// without writing the body, so we can detect 404s and serve index.html.
type spaResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	wroteBody   bool
}

func (w *spaResponseWriter) WriteHeader(code int) {
	w.status = code
	w.wroteHeader = true
	if code != http.StatusNotFound {
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *spaResponseWriter) Write(b []byte) (int, error) {
	if w.status == http.StatusNotFound {
		// Suppress the 404 body — we'll serve index.html instead
		return len(b), nil
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.wroteBody = true
	return w.ResponseWriter.Write(b)
}

func (w *spaResponseWriter) reset() {
	w.status = http.StatusOK
	w.wroteHeader = false
	w.wroteBody = false
}
