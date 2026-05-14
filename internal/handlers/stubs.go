package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

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
// @Description Returns system health status including database, antd, and queue
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Router /health [get]
func Health(db *database.DB, cfg *config.Config, antdInfo AntdInfoProvider) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	settingsSvc := services.NewCachedSettingsService(services.NewSettingsService(db))

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
		antdOK := false
		if cfg.AntdURL != "" {
			probeTimeout := time.Duration(settingsSvc.GetIntWithBounds(
				"antd_health_probe_timeout_secs", 15, 1, 120,
			)) * time.Second
			ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
			defer cancel()
			probe := antd.NewClient(cfg.AntdURL, antd.WithTimeout(probeTimeout))
			if _, err := probe.DataCost(ctx, []byte{0, 0, 0}); err == nil {
				antdOK = true
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

		body := map[string]any{
			"status":     statusText,
			"version":    buildinfo.Version,
			"database":   dbOK,
			"antd":       antdOK,
			"antd_url":   cfg.AntdURL,
			"queued":     queued,
			"processing": processing,
		}

		// Surface the antd /health snapshot when the daemon is managed and has
		// reported at least once. Unmanaged setups, pre-bootstrap probes, and
		// older antd builds (no diagnostic fields) all leave the antd_*
		// namespace unset rather than emitting zero values.
		if antdInfo != nil {
			if h := antdInfo.AntdInfo(); h != nil {
				body["antd_version"] = h.Version
				body["antd_evm_network"] = h.EvmNetwork
				body["antd_uptime_seconds"] = h.UptimeSeconds
				body["antd_build_commit"] = h.BuildCommit
				body["antd_payment_token_address"] = h.PaymentTokenAddress
				body["antd_payment_vault_address"] = h.PaymentVaultAddress
			}
		}

		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
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
