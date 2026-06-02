package handlers

import (
	"context"
	"net/http"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/buildinfo"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/services"
)

// @Summary      Check for updates
// @Description  Compare the running indelible + antd versions against the latest GitHub releases. On-demand (admin button); degrades gracefully when GitHub is unreachable.
// @Tags         Admin: System
// @Produce      json
// @Success      200 {object} services.VersionCheckResult
// @Router       /admin/version-check [get]
// @Security     BearerAuth
func AdminVersionCheck(cfg *config.Config, antdInfo AntdInfoProvider) http.HandlerFunc {
	svc := services.NewVersionCheckService()

	return func(w http.ResponseWriter, r *http.Request) {
		res := svc.Check(r.Context(), buildinfo.Version, currentAntdVersion(r.Context(), cfg, antdInfo))
		jsonResponse(w, http.StatusOK, res)
	}
}

// currentAntdVersion resolves the running antd version. In managed mode the
// last-known snapshot carries it; otherwise (the default separate-container
// setup) we ask antd's own /health. Returns "" if the version can't be
// determined, in which case the version check reports antd as unchecked.
func currentAntdVersion(ctx context.Context, cfg *config.Config, antdInfo AntdInfoProvider) string {
	if antdInfo != nil {
		if h := antdInfo.AntdInfo(); h != nil && h.Version != "" {
			return h.Version
		}
	}
	if cfg.AntdURL == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := antd.NewClient(cfg.AntdURL, antd.WithTimeout(5*time.Second))
	if h, err := probe.Health(cctx); err == nil {
		return h.Version
	}
	return ""
}
