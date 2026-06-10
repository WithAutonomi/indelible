package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/buildinfo"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

// AntdInfoProvider exposes the last-known antd /health snapshot. The
// production implementation is *internal/antd.Manager; tests pass a fake or
// nil when antd is unmanaged.
type AntdInfoProvider interface {
	AntdInfo() *antd.HealthStatus
}

// --- Health ---

// Health godoc
// @Summary Health check
// @Description Public liveness (status/database/antd + 200/503). Full diagnostics (version, antd_url, queue depth, antd_* network/build/payment detail) are returned only to an authenticated admin.
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Router /health [get]
func Health(db *database.DB, cfg *config.Config, antdInfo AntdInfoProvider) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	settingsSvc := services.NewCachedSettingsService(services.NewSettingsService(db))
	permSvc := services.NewPermissionService(db)
	// The active notifier method is recomputed per request rather than cached
	// because it can change at runtime via the admin "notifier_method" setting.
	notifierFor := func() services.NotifierMethodName {
		return services.NewNotifier(cfg, db).Method()
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check DB connectivity
		dbOK := db.Ping() == nil

		// Probe antd overlay connectivity with a tiny DataCost call.
		// Success means antd is reachable AND has peers to quote from;
		// any error (transport, 502 NetworkError, 503 ServiceUnavailable)
		// means antd is not usable for real uploads/downloads.
		// Payload must be >= 3 bytes: antd's self-encryption rejects smaller.
		// Timeout is read from antd_health_probe_timeout_secs (default 15,
		// bounds 1-120) so operators can tighten or loosen the alert SLA.
		probeTimeout := time.Duration(settingsSvc.GetIntWithBounds(
			"antd_health_probe_timeout_secs", 15, 1, 120,
		)) * time.Second
		antdOK := false
		if cfg.AntdURL != "" {
			ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
			defer cancel()
			probe := antd.NewClient(cfg.AntdURL, antd.WithTimeout(probeTimeout))
			if _, err := probe.DataCost(ctx, []byte{0, 0, 0}, antd.PaymentModeAuto); err == nil {
				antdOK = true
			}
		}

		// DB is the hard requirement; antd is informational
		status := http.StatusOK
		statusText := "healthy"
		if !dbOK {
			status = http.StatusServiceUnavailable
			statusText = "unhealthy"
		} else if !antdOK {
			statusText = "degraded"
		}

		// Public liveness surface: the 200/503 signal monitors rely on plus the
		// coarse status/database/antd booleans. Everything an unauthenticated
		// caller could mine for recon — version, build commit, EVM network,
		// payment-contract addresses, antd_url, queue depth — is admin-only
		// below (V2-485).
		body := map[string]any{
			"status":   statusText,
			"database": dbOK,
			"antd":     antdOK,
		}

		// Detailed diagnostics only for an authenticated admin. The admin System
		// page sends the session cookie automatically; anonymous liveness probes
		// don't, so they never see this block.
		if healthRequesterIsAdmin(r, cfg, permSvc) {
			counts, _ := uploadSvc.CountByStatus()
			body["version"] = buildinfo.Version
			body["antd_url"] = cfg.AntdURL
			body["queued"] = counts["queued"]
			body["processing"] = counts["processing"]
			body["notifier"] = string(notifierFor())

			// antd diagnostic snapshot — managed-mode cache, or a live antd
			// /health read for the default separate-container setup. Older antd
			// builds (no diagnostic fields) leave the antd_* namespace unset
			// rather than emitting zero values.
			var antdHealth *antd.HealthStatus
			if antdInfo != nil {
				antdHealth = antdInfo.AntdInfo()
			}
			if antdHealth == nil && cfg.AntdURL != "" {
				ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
				defer cancel()
				probe := antd.NewClient(cfg.AntdURL, antd.WithTimeout(probeTimeout))
				if h, err := probe.Health(ctx); err == nil {
					antdHealth = h
				}
			}
			if antdHealth != nil {
				body["antd_version"] = antdHealth.Version
				body["antd_evm_network"] = antdHealth.EvmNetwork
				body["antd_uptime_seconds"] = antdHealth.UptimeSeconds
				body["antd_build_commit"] = antdHealth.BuildCommit
				body["antd_payment_token_address"] = antdHealth.PaymentTokenAddress
				body["antd_payment_vault_address"] = antdHealth.PaymentVaultAddress
			}
		}

		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}
}

// healthRequesterIsAdmin reports whether the request carries a valid admin
// session (session cookie or bearer JWT). /health is a public route, so this is
// a best-effort, optional check used only to decide whether to include the
// admin-only diagnostic detail — it never rejects the request. API-token
// callers (non-JWT bearer) read as non-admin here and get the thin response;
// they can use the /admin endpoints for detail.
func healthRequesterIsAdmin(r *http.Request, cfg *config.Config, permSvc *services.PermissionService) bool {
	var tokenStr string
	if c, err := r.Cookie("session"); err == nil && c.Value != "" {
		tokenStr = c.Value
	} else if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		tokenStr = strings.TrimPrefix(ah, "Bearer ")
	}
	if tokenStr == "" {
		return false
	}
	jwtKR := cfg.JWTKeyring()
	claims, err := auth.ValidateToken(jwtKR.Primary(), tokenStr, jwtKR.Previous()...)
	if err != nil {
		return false
	}
	isAdmin, err := permSvc.IsAdmin(claims.UserID)
	return err == nil && isAdmin
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
