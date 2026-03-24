package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/services"
)

// errGasBackoff is a sentinel error indicating the upload should be retried later
// because gas fees are too high.
var errGasBackoff = errors.New("gas backoff")

// UploadWorker processes queued file uploads in the background.
type UploadWorker struct {
	uploadSvc   *services.UploadService
	txnSvc      *services.TransactionService
	walletSvc   *services.WalletService
	webhookSvc  *services.WebhookDeliveryService
	settingsSvc *services.SettingsService
	antdClient  *antd.Client
	cfg         *config.Config
	wg          sync.WaitGroup
	cancel      context.CancelFunc
}

// NewUploadWorker creates a new background upload processor.
func NewUploadWorker(db *sql.DB, cfg *config.Config) *UploadWorker {
	return &UploadWorker{
		uploadSvc:   services.NewUploadService(db),
		txnSvc:      services.NewTransactionService(db),
		walletSvc:   services.NewWalletService(db, cfg.WalletEncryptionKey),
		webhookSvc:  services.NewWebhookDeliveryService(db),
		settingsSvc: services.NewSettingsService(db),
		antdClient:  antd.NewClient(cfg.AntdURL),
		cfg:         cfg,
	}
}

// Start begins the upload processing loop and the stuck-upload reconciliation loop.
func (w *UploadWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	// Requeue any uploads left in "processing" from a previous crash
	requeued, err := w.uploadSvc.RequeueStuck(0) // immediate: anything still "processing" on startup
	if err != nil {
		slog.Error("crash recovery requeue failed", "error", err)
	} else if requeued > 0 {
		slog.Info("crash recovery: requeued stuck uploads", "count", requeued)
	}

	// Main processing loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.processLoop(ctx)
	}()

	// Reconciliation loop (check for stuck uploads every 5 minutes)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.reconcileLoop(ctx)
	}()

	slog.Info("upload worker started")
}

// Stop gracefully shuts down the worker, waiting for in-flight uploads.
func (w *UploadWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	slog.Info("upload worker stopped")
}

func (w *UploadWorker) getMaxConcurrent() int {
	val, err := w.settingsSvc.Get("max_concurrent_uploads")
	if err == nil {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func (w *UploadWorker) processLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var inFlight sync.WaitGroup
	sem := make(chan struct{}, 16) // hard cap; effective limit from settings

	for {
		select {
		case <-ctx.Done():
			inFlight.Wait()
			return
		case <-ticker.C:
			maxC := w.getMaxConcurrent()
			// Try to fill up to maxConcurrent slots
			for len(sem) < maxC {
				upload, err := w.uploadSvc.DequeueNext()
				if err != nil {
					slog.Error("dequeue upload failed", "error", err)
					break
				}
				if upload == nil {
					break // nothing to process
				}

				sem <- struct{}{}
				inFlight.Add(1)
				go func(u *services.Upload) {
					defer func() { <-sem; inFlight.Done() }()
					w.processOne(ctx, u)
				}(upload)
			}
		}
	}
}

func (w *UploadWorker) processOne(ctx context.Context, upload *services.Upload) {
	slog.Info("processing upload", "uuid", upload.UUID, "filename", upload.OriginalFilename, "size", upload.FileSize)
	w.webhookSvc.FireUploadEvent("processing", upload)

	if err := w.processUpload(ctx, upload); err != nil {
		if errors.Is(err, errGasBackoff) {
			// Not a failure — gas is too high, backoff and retry later
			slog.Warn("upload gas backoff", "uuid", upload.UUID, "attempt", upload.BackoffAttempt+1)
			return
		}
		slog.Error("upload failed", "uuid", upload.UUID, "error", err)
		w.uploadSvc.MarkFailed(upload.ID, err.Error())
		upload.Status = "failed"
		w.webhookSvc.FireUploadEvent("failed", upload)
		w.cleanupTempFile(upload)
		return
	}

	upload.Status = "completed"
	w.webhookSvc.FireUploadEvent("completed", upload)
	w.cleanupTempFile(upload)
}

func (w *UploadWorker) processUpload(ctx context.Context, upload *services.Upload) error {
	if !upload.TempPath.Valid || upload.TempPath.String == "" {
		return fmt.Errorf("File not found on server")
	}

	tempPath := upload.TempPath.String

	// Verify temp file exists
	if _, err := os.Stat(tempPath); err != nil {
		return fmt.Errorf("File not found on server")
	}

	// Check gas fee against max_gas_fee — backoff if too high
	if maxFeeStr, err := w.settingsSvc.Get("max_gas_fee"); err == nil {
		if maxFee, err := strconv.ParseInt(maxFeeStr, 10, 64); err == nil && maxFee > 0 {
			isPublic := upload.Visibility == "public"
			quotedCost, err := w.antdClient.FileCost(ctx, tempPath, isPublic, true)
			if err != nil {
				return fmt.Errorf("Unable to check network fees")
			}
			var costVal int64
			fmt.Sscanf(quotedCost, "%d", &costVal)
			if costVal > maxFee {
				attempt := upload.BackoffAttempt + 1
				if attempt > maxGasBackoffAttempts {
					return fmt.Errorf("Gas fees too high — try again later")
				}
				backoffUntil := calcGasBackoff(attempt)
				if err := w.uploadSvc.SetGasBackoff(upload.ID, backoffUntil, attempt, quotedCost); err != nil {
					return fmt.Errorf("Internal error scheduling retry")
				}
				slog.Info("gas fee too high, backing off",
					"uuid", upload.UUID, "quoted", quotedCost, "max", maxFeeStr,
					"attempt", attempt, "retry_at", backoffUntil.Format(time.RFC3339))
				return errGasBackoff
			}
			// Gas fee acceptable — clear any previous backoff state
			if upload.BackoffAttempt > 0 {
				w.uploadSvc.ClearBackoff(upload.ID)
				slog.Info("gas fee acceptable after backoff", "uuid", upload.UUID, "quoted", quotedCost, "attempts", upload.BackoffAttempt)
			}
			slog.Info("gas fee check passed", "uuid", upload.UUID, "quoted", quotedCost, "max", maxFeeStr)
		}
	}

	var result *antd.PutResult
	var err error

	if upload.Visibility == "public" {
		result, err = w.antdClient.FileUploadPublic(ctx, tempPath)
	} else {
		// Private upload: read file bytes and use DataPutPrivate
		data, readErr := os.ReadFile(tempPath)
		if readErr != nil {
			return fmt.Errorf("File not found on server")
		}
		result, err = w.antdClient.DataPutPrivate(ctx, data)
	}

	if err != nil {
		return fmt.Errorf("Network upload failed")
	}

	// Mark upload completed
	if err := w.uploadSvc.MarkCompleted(upload.ID, result.Address, result.Cost); err != nil {
		return fmt.Errorf("Failed to save upload record")
	}

	// Record transaction against default wallet (best-effort)
	if wallet, err := w.walletSvc.GetDefault(); err == nil {
		w.txnSvc.Record(wallet.ID, &upload.ID, "upload", result.Cost, wallet.PaymentBalance)
	}

	slog.Info("upload completed", "uuid", upload.UUID, "address", result.Address, "cost", result.Cost)
	return nil
}

func (w *UploadWorker) cleanupTempFile(upload *services.Upload) {
	if !upload.TempPath.Valid || upload.TempPath.String == "" {
		return
	}
	if err := os.Remove(upload.TempPath.String); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to clean up temp file", "path", upload.TempPath.String, "error", err)
	}
}

func (w *UploadWorker) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requeued, err := w.uploadSvc.RequeueStuck(2) // stuck > 2 minutes
			if err != nil {
				slog.Error("reconciliation requeue failed", "error", err)
			} else if requeued > 0 {
				slog.Info("reconciliation: requeued stuck uploads", "count", requeued)
			}
		}
	}
}

// maxGasBackoffAttempts is the number of retries before giving up.
const maxGasBackoffAttempts = 10

// calcGasBackoff returns the time to retry based on the attempt number.
// Strategy:
//   - Attempts 1-3: short exponential backoff (5m, 15m, 45m) — covers transient spikes
//   - Attempts 4-6: longer backoff (2h, 4h, 6h) — waits for intra-day cycle relief
//   - Attempts 7+:  schedule for next "cheap window" (02:00-06:00 UTC) — gas fees
//     on most blockchains follow a 24h cycle with lows overnight UTC
func calcGasBackoff(attempt int) time.Time {
	now := time.Now().UTC()

	switch {
	case attempt <= 1:
		return now.Add(5 * time.Minute)
	case attempt == 2:
		return now.Add(15 * time.Minute)
	case attempt == 3:
		return now.Add(45 * time.Minute)
	case attempt == 4:
		return now.Add(2 * time.Hour)
	case attempt == 5:
		return now.Add(4 * time.Hour)
	case attempt == 6:
		return now.Add(6 * time.Hour)
	default:
		// Schedule for next 02:00 UTC (start of cheap window)
		return nextCheapWindow(now)
	}
}

// nextCheapWindow returns the next 02:00 UTC. If we're currently in the
// cheap window (02:00-06:00), returns the next day's window since the current
// one clearly isn't cheap enough.
func nextCheapWindow(now time.Time) time.Time {
	target := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, time.UTC)
	// If we're past 02:00 today (or in the current cheap window that didn't help),
	// schedule for tomorrow
	if now.Hour() >= 2 {
		target = target.Add(24 * time.Hour)
	}
	return target
}

// TempUploadDir returns the path to the temp upload directory, creating it if needed.
func TempUploadDir(cfg *config.Config) string {
	dir := filepath.Join(cfg.DataDir, "uploads", "tmp")
	os.MkdirAll(dir, 0750)
	return dir
}
