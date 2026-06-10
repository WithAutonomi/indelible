package handlers

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/buildinfo"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/diskusage"
	"github.com/WithAutonomi/indelible/internal/services"
)

// StorageInfo describes data-directory disk usage for the admin System page.
type StorageInfo struct {
	DataDir    string        `json:"data_dir"`             // configured data directory path
	Volume     string        `json:"volume"`               // drive letter on Windows (e.g. "C:"), empty on Unix
	Available  bool          `json:"available"`            // false when disk figures couldn't be read
	TotalBytes uint64        `json:"total_bytes"`          // capacity of the backing filesystem
	UsedBytes  uint64        `json:"used_bytes"`           // bytes in use on that filesystem
	FreeBytes  uint64        `json:"free_bytes"`           // bytes free on that filesystem
	UsedPct    float64       `json:"used_pct"`             // used / total * 100
	Quota      *StorageQuota `json:"quota,omitempty"`      // present only when a system storage quota is set
}

// StorageQuota is the used-vs-limit view of the configured system-wide quota.
type StorageQuota struct {
	MaxBytes  int64   `json:"max_bytes"`
	UsedBytes int64   `json:"used_bytes"`
	UsedPct   float64 `json:"used_pct"`
}

// @Summary      Storage usage
// @Description  Disk usage of the data directory (used/free/total bytes + percent) and, when a system-wide storage quota is configured, used-vs-quota. Degrades gracefully when disk figures can't be read.
// @Tags         Admin: System
// @Produce      json
// @Success      200 {object} handlers.StorageInfo
// @Router       /admin/storage [get]
// @Security     BearerAuth
func AdminStorage(cfg *config.Config, db *database.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		info := StorageInfo{
			DataDir: cfg.DataDir,
			Volume:  filepath.VolumeName(cfg.DataDir),
		}

		if total, free, used, ok := diskusage.Usage(cfg.DataDir); ok && total > 0 {
			info.Available = true
			info.TotalBytes = total
			info.FreeBytes = free
			info.UsedBytes = used
			info.UsedPct = float64(used) / float64(total) * 100.0
		}

		// System quota is optional: omit the field entirely when unset/disabled
		// so the UI can hide the line rather than render a zero/broken bar.
		if q, err := quotaSvc.SystemQuota(); err == nil && q != nil && q.MaxBytes > 0 {
			info.Quota = &StorageQuota{
				MaxBytes:  q.MaxBytes,
				UsedBytes: q.UsedBytes,
				UsedPct:   float64(q.UsedBytes) / float64(q.MaxBytes) * 100.0,
			}
		}

		jsonResponse(w, http.StatusOK, info)
	}
}

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
