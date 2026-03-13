package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/services"
)

// DiskAlertWorker monitors data directory disk usage and fires alerts.
type DiskAlertWorker struct {
	cfg    *config.Config
	logSvc *services.LogService
	wg     sync.WaitGroup
	cancel context.CancelFunc

	// IsPaused indicates uploads should be paused due to critical disk usage.
	IsPaused bool
}

// NewDiskAlertWorker creates a new disk alert worker.
func NewDiskAlertWorker(db *sql.DB, cfg *config.Config) *DiskAlertWorker {
	return &DiskAlertWorker{
		cfg:    cfg,
		logSvc: services.NewLogService(db),
	}
}

// Start begins disk monitoring (every 5 minutes).
func (w *DiskAlertWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

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
	usagePct := getDiskUsagePercent(w.cfg.DataDir)
	if usagePct < 0 {
		return // couldn't determine
	}

	pctStr := fmt.Sprintf("%.1f", usagePct)

	if usagePct >= 95 {
		if !w.IsPaused {
			w.IsPaused = true
			w.logSvc.WriteSystem("error", "disk_alert",
				"Critical: disk usage at "+pctStr+"%, uploads paused", "")
			slog.Error("disk critical — uploads paused", "usage_pct", usagePct)
		}
	} else if usagePct >= 80 {
		w.IsPaused = false
		w.logSvc.WriteSystem("warn", "disk_alert",
			"Warning: disk usage at "+pctStr+"%", "")
		slog.Warn("disk warning", "usage_pct", usagePct)
	} else {
		if w.IsPaused {
			w.IsPaused = false
			w.logSvc.WriteSystem("info", "disk_alert",
				"Disk usage back to normal ("+pctStr+"%)", "")
			slog.Info("disk usage normal — uploads resumed", "usage_pct", usagePct)
		}
	}
}
