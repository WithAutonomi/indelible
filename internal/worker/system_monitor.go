package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/services"
)

// SystemMonitor runs periodic health checks and fires webhook alerts.
// Consolidates: antd health, EVM RPC health, wallet balance, queue backlog,
// failure rate, webhook delivery failures, gas backoff count, stale uploads,
// temp directory size, and worker liveness.
type SystemMonitor struct {
	cfg        *config.Config
	db         *sql.DB
	uploadSvc  *services.UploadService
	walletSvc  *services.WalletService
	settingsSvc *services.SettingsService
	logSvc     *services.LogService
	webhookSvc *services.WebhookDeliveryService

	wg     sync.WaitGroup
	cancel context.CancelFunc

	// Alert deduplication — only fire when state changes
	lastAlerts map[string]string // check name → last alert level
	mu         sync.Mutex

	// Worker liveness tracking
	lastDequeueTime time.Time
}

// NewSystemMonitor creates a new consolidated system monitor.
func NewSystemMonitor(db *sql.DB, cfg *config.Config) *SystemMonitor {
	return &SystemMonitor{
		cfg:         cfg,
		db:          db,
		uploadSvc:   services.NewUploadService(db),
		walletSvc:   services.NewWalletService(db, cfg.WalletEncryptionKey),
		settingsSvc: services.NewSettingsService(db),
		logSvc:      services.NewLogService(db),
		webhookSvc:  services.NewWebhookDeliveryService(db),
		lastAlerts:  make(map[string]string),
	}
}

// RecordDequeue is called by the upload worker when it successfully dequeues an upload.
// Used for worker liveness detection.
func (m *SystemMonitor) RecordDequeue() {
	m.mu.Lock()
	m.lastDequeueTime = time.Now()
	m.mu.Unlock()
}

// Start begins all monitoring loops.
func (m *SystemMonitor) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	// Fast checks: every 30 seconds
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runLoop(ctx, 30*time.Second, m.fastChecks)
	}()

	// Slow checks: every 5 minutes
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runLoop(ctx, 5*time.Minute, m.slowChecks)
	}()

	slog.Info("system monitor started")
}

// Stop gracefully shuts down all monitoring.
func (m *SystemMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *SystemMonitor) runLoop(ctx context.Context, interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}

// fastChecks run every 30 seconds — lightweight, time-sensitive checks.
func (m *SystemMonitor) fastChecks() {
	m.checkAntdHealth()
	m.checkWorkerLiveness()
	m.checkStaleUploads()
}

// slowChecks run every 5 minutes — heavier or less urgent checks.
func (m *SystemMonitor) slowChecks() {
	m.checkWalletBalance()
	m.checkQueueBacklog()
	m.checkFailureRate()
	m.checkEvmRpcHealth()
	m.checkWebhookDeliveryFailures()
	m.checkGasBackoffCount()
	m.checkTempDirSize()
}

// --- Individual Checks ---

func (m *SystemMonitor) checkAntdHealth() {
	if m.cfg.AntdURL == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", m.cfg.AntdURL+"/v1/node/version", nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)

	if err != nil || resp.StatusCode >= 500 {
		m.fireAlert("antd_health", "critical", "antd_unreachable",
			"antd daemon is unreachable at "+m.cfg.AntdURL, 0)
	} else {
		resp.Body.Close()
		m.clearAlert("antd_health", "antd_recovered",
			"antd daemon is reachable again", 0)
	}
}

func (m *SystemMonitor) checkEvmRpcHealth() {
	if m.cfg.EvmRPCURL == "" {
		return // not yet configured (set during first upload)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", m.cfg.EvmRPCURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		m.fireAlert("evm_rpc", "critical", "evm_rpc_unreachable",
			"EVM RPC endpoint is unreachable at "+m.cfg.EvmRPCURL, 0)
	} else {
		resp.Body.Close()
		m.clearAlert("evm_rpc", "evm_rpc_recovered",
			"EVM RPC endpoint is reachable again", 0)
	}
}

func (m *SystemMonitor) checkWalletBalance() {
	wallet, err := m.walletSvc.GetDefault()
	if err != nil || wallet == nil {
		return
	}

	// Read threshold from settings (default: 0 = disabled)
	thresholdStr, _ := m.settingsSvc.Get("wallet_balance_alert_threshold")
	if thresholdStr == "" || thresholdStr == "0" {
		return
	}

	// Parse balances as big integers (atto-tokens)
	balance := parseAtto(wallet.PaymentBalance)
	threshold := parseAtto(thresholdStr)

	if threshold > 0 && balance < threshold {
		msg := fmt.Sprintf("Wallet balance is low: %s (threshold: %s)", wallet.PaymentBalance, thresholdStr)
		m.fireAlert("wallet_balance", "warning", "wallet_balance_low", msg, float64(balance))
	} else {
		m.clearAlert("wallet_balance", "wallet_balance_ok",
			"Wallet balance is above threshold", float64(balance))
	}
}

func (m *SystemMonitor) checkQueueBacklog() {
	counts, err := m.uploadSvc.CountByStatus()
	if err != nil {
		return
	}

	queued := counts["queued"]
	processing := counts["processing"]
	backlog := queued + processing

	// Alert if backlog exceeds max_queued_uploads * 80%
	maxQueuedStr, _ := m.settingsSvc.Get("max_queued_uploads")
	maxQueued := int64(500)
	if n, err := strconv.ParseInt(maxQueuedStr, 10, 64); err == nil && n > 0 {
		maxQueued = n
	}

	threshold := maxQueued * 80 / 100
	if backlog > threshold {
		msg := fmt.Sprintf("Upload queue backlog is high: %d items (threshold: %d)", backlog, threshold)
		m.fireAlert("queue_backlog", "warning", "queue_backlog_high", msg, float64(backlog))
	} else {
		m.clearAlert("queue_backlog", "queue_backlog_normal",
			fmt.Sprintf("Queue backlog normal: %d items", backlog), float64(backlog))
	}
}

func (m *SystemMonitor) checkFailureRate() {
	// Count recent failures (last 15 minutes)
	var recentFail, recentTotal int64
	m.db.QueryRow(
		`SELECT COUNT(*) FROM uploads WHERE failed_at > datetime('now', '-15 minutes')`,
	).Scan(&recentFail)
	m.db.QueryRow(
		`SELECT COUNT(*) FROM uploads WHERE created_at > datetime('now', '-15 minutes')`,
	).Scan(&recentTotal)

	if recentTotal < 5 {
		return // not enough data
	}

	failRate := float64(recentFail) / float64(recentTotal) * 100
	if failRate > 25 {
		msg := fmt.Sprintf("Upload failure rate is high: %.0f%% (%d/%d in last 15 min)", failRate, recentFail, recentTotal)
		m.fireAlert("failure_rate", "warning", "failure_rate_high", msg, failRate)
	} else {
		m.clearAlert("failure_rate", "failure_rate_normal",
			fmt.Sprintf("Upload failure rate normal: %.0f%%", failRate), failRate)
	}
}

func (m *SystemMonitor) checkWebhookDeliveryFailures() {
	// Count failed deliveries in last hour
	var failed, total int64
	m.db.QueryRow(
		`SELECT COUNT(*) FROM webhook_delivery_log WHERE created_at > datetime('now', '-1 hour') AND success = 0`,
	).Scan(&failed)
	m.db.QueryRow(
		`SELECT COUNT(*) FROM webhook_delivery_log WHERE created_at > datetime('now', '-1 hour')`,
	).Scan(&total)

	if total < 3 {
		return
	}

	failRate := float64(failed) / float64(total) * 100
	if failRate > 50 {
		msg := fmt.Sprintf("Webhook delivery failure rate: %.0f%% (%d/%d in last hour)", failRate, failed, total)
		m.fireAlert("webhook_delivery", "warning", "webhook_delivery_failing", msg, failRate)
	} else {
		m.clearAlert("webhook_delivery", "webhook_delivery_ok",
			"Webhook delivery rate normal", failRate)
	}
}

func (m *SystemMonitor) checkGasBackoffCount() {
	var count int64
	m.db.QueryRow(
		`SELECT COUNT(*) FROM uploads WHERE status_detail = 'gas_backoff'`,
	).Scan(&count)

	if count >= 10 {
		msg := fmt.Sprintf("%d uploads waiting in gas backoff — network costs may be elevated", count)
		m.fireAlert("gas_backoff", "warning", "gas_backoff_high", msg, float64(count))
	} else {
		m.clearAlert("gas_backoff", "gas_backoff_normal",
			fmt.Sprintf("Gas backoff count normal: %d", count), float64(count))
	}
}

func (m *SystemMonitor) checkStaleUploads() {
	var count int64
	m.db.QueryRow(
		`SELECT COUNT(*) FROM uploads WHERE status = 'processing' AND processing_at < datetime('now', '-10 minutes')`,
	).Scan(&count)

	if count > 0 {
		msg := fmt.Sprintf("%d uploads stuck in processing for >10 minutes", count)
		m.fireAlert("stale_uploads", "warning", "stale_uploads_detected", msg, float64(count))
	} else {
		m.clearAlert("stale_uploads", "stale_uploads_clear", "No stale uploads", 0)
	}
}

func (m *SystemMonitor) checkTempDirSize() {
	tempDir := TempUploadDir(m.cfg)
	var totalSize int64
	filepath.Walk(tempDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		totalSize += info.Size()
		return nil
	})

	// Alert if temp dir > 10 GB
	const threshold = 10 * 1024 * 1024 * 1024
	if totalSize > threshold {
		sizeGB := float64(totalSize) / (1024 * 1024 * 1024)
		msg := fmt.Sprintf("Temp upload directory is %.1f GB — files may not be cleaning up", sizeGB)
		m.fireAlert("temp_dir_size", "warning", "temp_dir_large", msg, sizeGB)
	} else {
		m.clearAlert("temp_dir_size", "temp_dir_ok", "Temp directory size normal", float64(totalSize))
	}
}

func (m *SystemMonitor) checkWorkerLiveness() {
	m.mu.Lock()
	lastDequeue := m.lastDequeueTime
	m.mu.Unlock()

	if lastDequeue.IsZero() {
		return // worker hasn't started yet or nothing to process
	}

	// Check if there are queued items but no dequeue in 5 minutes
	counts, err := m.uploadSvc.CountByStatus()
	if err != nil {
		return
	}
	if counts["queued"] == 0 {
		m.clearAlert("worker_liveness", "worker_alive", "Upload worker is processing normally", 0)
		return
	}

	if time.Since(lastDequeue) > 5*time.Minute {
		msg := fmt.Sprintf("Upload worker appears stuck — %d queued items but no dequeue in %.0f minutes",
			counts["queued"], time.Since(lastDequeue).Minutes())
		m.fireAlert("worker_liveness", "critical", "worker_stuck", msg, time.Since(lastDequeue).Minutes())
	} else {
		m.clearAlert("worker_liveness", "worker_alive", "Upload worker is processing normally", 0)
	}
}

// --- Alert Infrastructure ---

func (m *SystemMonitor) fireAlert(checkName, level, eventType, message string, value float64) {
	m.mu.Lock()
	prev := m.lastAlerts[checkName]
	m.mu.Unlock()

	if prev == level {
		return // already alerted at this level, deduplicate
	}

	m.mu.Lock()
	m.lastAlerts[checkName] = level
	m.mu.Unlock()

	slog.Warn("system monitor alert", "check", checkName, "level", level, "message", message)
	m.logSvc.WriteSystem(level, "system_monitor", message, "")
	m.webhookSvc.FireSystemEvent(eventType, &services.WebhookSystemData{
		AlertType: eventType,
		Message:   message,
		Value:     value,
	})
}

func (m *SystemMonitor) clearAlert(checkName, eventType, message string, value float64) {
	m.mu.Lock()
	prev := m.lastAlerts[checkName]
	m.mu.Unlock()

	if prev == "" {
		return // was never alerted, nothing to clear
	}

	m.mu.Lock()
	delete(m.lastAlerts, checkName)
	m.mu.Unlock()

	slog.Info("system monitor recovered", "check", checkName, "message", message)
	m.logSvc.WriteSystem("info", "system_monitor", message, "")
	m.webhookSvc.FireSystemEvent(eventType, &services.WebhookSystemData{
		AlertType: eventType,
		Message:   message,
		Value:     value,
	})
}

// parseAtto parses a numeric string (atto-token balance) to int64.
func parseAtto(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
