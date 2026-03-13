package worker

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/maidsafe/indelible/internal/services"
)

// LogRetentionWorker periodically cleans up old logs based on system settings.
type LogRetentionWorker struct {
	logSvc      *services.LogService
	settingsSvc *services.SettingsService
	wg          sync.WaitGroup
	cancel      context.CancelFunc
}

// NewLogRetentionWorker creates a new log retention worker.
func NewLogRetentionWorker(db *sql.DB) *LogRetentionWorker {
	return &LogRetentionWorker{
		logSvc:      services.NewLogService(db),
		settingsSvc: services.NewSettingsService(db),
	}
}

// Start begins the log retention worker (runs hourly).
func (w *LogRetentionWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.loop(ctx)
	}()

	slog.Info("log retention worker started")
}

// Stop gracefully shuts down the worker.
func (w *LogRetentionWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	slog.Info("log retention worker stopped")
}

func (w *LogRetentionWorker) loop(ctx context.Context) {
	// Run immediately on startup
	w.run()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.run()
		}
	}
}

func (w *LogRetentionWorker) run() {
	enabled, _ := w.settingsSvc.Get("log_retention_enabled")
	if enabled != "true" {
		return
	}

	daysStr, _ := w.settingsSvc.Get("log_retention_days")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	deleted, err := w.logSvc.CleanupOldLogs(days)
	if err != nil {
		slog.Error("log retention cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("log retention: cleaned up old logs", "deleted", deleted, "retention_days", days)
	}
}
