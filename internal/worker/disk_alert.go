package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/diskusage"
	"github.com/WithAutonomi/indelible/internal/services"
)

// UploadsPausedSetting is the settings key the worker writes its pause state to.
// CreateUpload reads it to shed load when the disk is critically full. Lives in
// the settings table (not just the in-memory IsPaused flag) so the HTTP handler,
// which has no reference to this worker, can consult it — and so the state
// survives a restart until the next check reconciles it.
const UploadsPausedSetting = "uploads_paused"

// DiskAlertWorker monitors data directory disk usage and fires alerts.
type DiskAlertWorker struct {
	cfg         *config.Config
	logSvc      *services.LogService
	webhookSvc  *services.WebhookDeliveryService
	settingsSvc *services.SettingsService
	wg          sync.WaitGroup
	cancel      context.CancelFunc

	// IsPaused indicates uploads should be paused due to critical disk usage.
	IsPaused bool

	// lastAlertLevel tracks the last alert fired to avoid spamming.
	// Values: "", "warning", "critical"
	lastAlertLevel string
}

// NewDiskAlertWorker creates a new disk alert worker.
func NewDiskAlertWorker(db *database.DB, cfg *config.Config) *DiskAlertWorker {
	return &DiskAlertWorker{
		cfg:         cfg,
		logSvc:      services.NewLogService(db),
		webhookSvc:  services.NewWebhookDeliveryService(db),
		settingsSvc: services.NewSettingsService(db),
	}
}

// setPaused updates the in-memory flag and persists it to settings, but only on
// an actual transition so we don't write to the DB on every 5-minute tick.
func (w *DiskAlertWorker) setPaused(paused bool) {
	if w.IsPaused == paused {
		return
	}
	w.IsPaused = paused
	val := "false"
	if paused {
		val = "true"
	}
	if err := w.settingsSvc.SetInternal(UploadsPausedSetting, val); err != nil {
		slog.Error("failed to persist uploads_paused state", "paused", paused, "error", err)
	}
}

// Start begins disk monitoring (every 5 minutes).
func (w *DiskAlertWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	// Reconcile persisted pause state across restarts: load the last-known flag,
	// then run one immediate check so a stale "paused" doesn't outlive recovery
	// (and a still-critical disk re-pauses without waiting a full tick).
	if v, err := w.settingsSvc.Get(UploadsPausedSetting); err == nil {
		w.IsPaused = v == "true"
	}
	w.check()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.loop(ctx)
	}()

	slog.Info("disk alert worker started")
}

// Stop gracefully shuts down the worker.
func (w *DiskAlertWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

func (w *DiskAlertWorker) loop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *DiskAlertWorker) check() {
	total, _, used, ok := diskusage.Usage(w.cfg.DataDir)
	if !ok || total == 0 {
		return // couldn't determine
	}
	usagePct := float64(used) / float64(total) * 100.0

	pctStr := fmt.Sprintf("%.1f", usagePct)

	switch {
	case usagePct >= 95:
		if !w.IsPaused {
			w.setPaused(true)
			w.logSvc.WriteSystem("error", "disk_alert",
				"Critical: disk usage at "+pctStr+"%, uploads paused", "", "")
			slog.Error("disk critical — uploads paused", "usage_pct", usagePct)
		}
		if w.lastAlertLevel != "critical" {
			w.lastAlertLevel = "critical"
			w.webhookSvc.FireSystemEvent("disk_critical", &services.WebhookSystemData{
				AlertType: "disk_critical",
				Message:   "Disk usage at " + pctStr + "%, uploads paused",
				Value:     usagePct,
			})
		}
	case usagePct >= 80:
		w.setPaused(false)
		w.logSvc.WriteSystem("warn", "disk_alert",
			"Warning: disk usage at "+pctStr+"%", "", "")
		slog.Warn("disk warning", "usage_pct", usagePct)
		if w.lastAlertLevel != "warning" {
			w.lastAlertLevel = "warning"
			w.webhookSvc.FireSystemEvent("disk_warning", &services.WebhookSystemData{
				AlertType: "disk_warning",
				Message:   "Disk usage at " + pctStr + "%",
				Value:     usagePct,
			})
		}
	default:
		if w.IsPaused {
			w.setPaused(false)
			w.logSvc.WriteSystem("info", "disk_alert",
				"Disk usage back to normal ("+pctStr+"%)", "", "")
			slog.Info("disk usage normal — uploads resumed", "usage_pct", usagePct)
		}
		if w.lastAlertLevel != "" {
			w.webhookSvc.FireSystemEvent("disk_recovered", &services.WebhookSystemData{
				AlertType: "disk_recovered",
				Message:   "Disk usage back to normal (" + pctStr + "%)",
				Value:     usagePct,
			})
			w.lastAlertLevel = ""
		}
	}
}
