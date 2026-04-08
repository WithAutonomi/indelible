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
	"github.com/WithAutonomi/indelible/internal/evm"
	"github.com/WithAutonomi/indelible/internal/services"
)

// isTransientAntdError returns true for antd errors that may succeed on retry.
func isTransientAntdError(err error) bool {
	var netErr *antd.NetworkError
	var unavailErr *antd.ServiceUnavailableError
	return errors.As(err, &netErr) || errors.As(err, &unavailErr)
}

// isPermanentAntdError returns true for antd errors that will never succeed on retry.
func isPermanentAntdError(err error) bool {
	var badReq *antd.BadRequestError
	var tooLarge *antd.TooLargeError
	return errors.As(err, &badReq) || errors.As(err, &tooLarge)
}

// errGasBackoff is a sentinel error indicating the upload should be retried later
// because gas fees are too high.
var errGasBackoff = errors.New("gas backoff")

// UploadWorker processes queued file uploads in the background.
type UploadWorker struct {
	uploadSvc   *services.UploadService
	quotaSvc    *services.QuotaService
	txnSvc      *services.TransactionService
	walletSvc   *services.WalletService
	webhookSvc  *services.WebhookDeliveryService
	settingsSvc *services.CachedSettingsService
	antdClient  *antd.Client
	evmSigner   *evm.Signer // lazily initialized on first upload
	cfg         *config.Config
	wg          sync.WaitGroup
	cancel      context.CancelFunc

	// S9: Per-phase circuit breakers with exponential cooldown
	prepareFailures  int
	circuitOpenUntil time.Time
	circuitCooldown  time.Duration
}

const circuitBreakerThreshold = 5
const circuitBreakerBaseCooldown = 30 * time.Second
const circuitBreakerMaxCooldown = 5 * time.Minute

// NewUploadWorker creates a new background upload processor.
func NewUploadWorker(db *sql.DB, cfg *config.Config) *UploadWorker {
	return &UploadWorker{
		uploadSvc:       services.NewUploadService(db),
		quotaSvc:        services.NewQuotaService(db),
		txnSvc:          services.NewTransactionService(db),
		walletSvc:       services.NewWalletService(db, cfg.WalletEncryptionKey),
		webhookSvc:      services.NewWebhookDeliveryService(db),
		settingsSvc:     services.NewCachedSettingsService(services.NewSettingsService(db)),
		antdClient:      antd.NewClient(cfg.AntdURL),
		cfg:             cfg,
		circuitCooldown: circuitBreakerBaseCooldown,
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

	// S4: Temp file garbage collector (every 5 minutes)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.tempGCLoop(ctx)
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
	sem := make(chan struct{}, 32) // hard cap; effective limit from settings

	for {
		select {
		case <-ctx.Done():
			inFlight.Wait()
			return
		case <-ticker.C:
			// Circuit breaker: skip processing if antd is unreachable
			if time.Now().Before(w.circuitOpenUntil) {
				continue
			}

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

// maxTransientRetries is the number of times to retry a transient antd error
// before marking the upload as failed.
const maxTransientRetries = 3

func (w *UploadWorker) processOne(ctx context.Context, upload *services.Upload) {
	slog.Info("processing upload", "uuid", upload.UUID, "filename", upload.OriginalFilename, "size", upload.FileSize)
	w.webhookSvc.FireUploadEvent("processing", upload)

	var lastErr error
	for attempt := 0; attempt <= maxTransientRetries; attempt++ {
		lastErr = w.processUpload(ctx, upload)
		if lastErr == nil {
			w.prepareFailures = 0
			w.circuitCooldown = circuitBreakerBaseCooldown
			upload.Status = "completed"
			w.webhookSvc.FireUploadEvent("completed", upload)
			w.cleanupTempFile(upload)
			return
		}

		if errors.Is(lastErr, errGasBackoff) {
			slog.Warn("upload gas backoff", "uuid", upload.UUID, "attempt", upload.BackoffAttempt+1)
			return
		}

		// Permanent errors fail immediately
		if isPermanentAntdError(lastErr) || !isTransientAntdError(lastErr) {
			break
		}

		// Transient error — wait and retry
		if attempt < maxTransientRetries {
			delay := time.Duration(attempt+1) * 5 * time.Second
			slog.Warn("transient antd error, retrying", "uuid", upload.UUID, "attempt", attempt+1, "delay", delay, "error", lastErr)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}

	// S9: Track consecutive transient failures with exponential cooldown
	if isTransientAntdError(lastErr) {
		w.prepareFailures++
		if w.prepareFailures >= circuitBreakerThreshold {
			w.circuitOpenUntil = time.Now().Add(w.circuitCooldown)
			slog.Warn("circuit breaker opened — antd appears unreachable",
				"failures", w.prepareFailures, "cooldown", w.circuitCooldown)
			// Exponential cooldown: 30s → 60s → 120s → ... → 5min max
			w.circuitCooldown *= 2
			if w.circuitCooldown > circuitBreakerMaxCooldown {
				w.circuitCooldown = circuitBreakerMaxCooldown
			}
		}
	}

	slog.Error("upload failed", "uuid", upload.UUID, "error", lastErr)
	_ = w.uploadSvc.MarkFailed(upload.ID, lastErr.Error())
	upload.Status = "failed"
	w.webhookSvc.FireUploadEvent("failed", upload)
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

	// S3: Re-check quota at processing time (includes in-flight bytes)
	if err := w.quotaSvc.CheckUserQuotaInFlight(upload.UserID, upload.FileSize); err != nil {
		return fmt.Errorf("Quota exceeded: %w", err)
	}

	// Get default wallet — required for external signer payment
	wallet, err := w.walletSvc.GetDefault()
	if err != nil {
		return fmt.Errorf("No wallet configured for payment")
	}

	walletKey, err := w.walletSvc.DecryptKey(wallet)
	if err != nil {
		return fmt.Errorf("Failed to decrypt wallet key")
	}

	// Phase 1: Prepare upload — encrypts file, collects network quotes
	prepared, err := w.antdClient.PrepareUpload(ctx, tempPath)
	if err != nil {
		return fmt.Errorf("Failed to prepare upload: %w", err)
	}

	// Gas fee check — only applies to wave-batch where cost is known upfront.
	// Merkle cost is determined on-chain so we skip the pre-check.
	if prepared.PaymentType != "merkle" {
		if maxFeeStr, err := w.settingsSvc.Get("max_gas_fee"); err == nil {
			if maxFee, err := strconv.ParseInt(maxFeeStr, 10, 64); err == nil && maxFee > 0 {
				var costVal int64
				_, _ = fmt.Sscanf(prepared.TotalAmount, "%d", &costVal)
				if costVal > maxFee {
					attempt := upload.BackoffAttempt + 1
					if attempt > maxGasBackoffAttempts {
						return fmt.Errorf("Gas fees too high — try again later")
					}
					backoffUntil := calcGasBackoff(attempt)
					if err := w.uploadSvc.SetGasBackoff(upload.ID, backoffUntil, attempt, prepared.TotalAmount); err != nil {
						return fmt.Errorf("Internal error scheduling retry")
					}
					slog.Info("gas fee too high, backing off",
						"uuid", upload.UUID, "quoted", prepared.TotalAmount, "max", maxFeeStr,
						"attempt", attempt, "retry_at", backoffUntil.Format(time.RFC3339))
					return errGasBackoff
				}
				if upload.BackoffAttempt > 0 {
					_ = w.uploadSvc.ClearBackoff(upload.ID)
					slog.Info("gas fee acceptable after backoff", "uuid", upload.UUID, "quoted", prepared.TotalAmount, "attempts", upload.BackoffAttempt)
				}
				slog.Info("gas fee check passed", "uuid", upload.UUID, "quoted", prepared.TotalAmount, "max", maxFeeStr)
			}
		}
	}

	// Cache EVM config for balance queries and other uses
	if w.cfg.EvmRPCURL == "" && prepared.RPCUrl != "" {
		w.cfg.EvmRPCURL = prepared.RPCUrl
		w.cfg.EvmTokenAddress = prepared.PaymentTokenAddress
	}

	// Ensure EVM signer is connected
	if w.evmSigner == nil || w.evmSigner.RPCUrl() != prepared.RPCUrl {
		signer, err := evm.NewSigner(prepared.RPCUrl)
		if err != nil {
			return fmt.Errorf("Failed to connect to EVM RPC: %w", err)
		}
		w.evmSigner = signer
	}

	// Phase 2 + 3: Payment and finalization — branches on payment type
	var result *antd.FinalizeUploadResult
	var paidAmount string
	var txHash string

	switch prepared.PaymentType {
	case "merkle":
		// Phase 2: Sign merkle batch payment
		winnerHash, totalPaid, err := w.evmSigner.PayForMerkleTree(
			ctx, walletKey,
			prepared.Depth,
			prepared.PoolCommitments,
			prepared.MerklePaymentTimestamp,
			prepared.PaymentTokenAddress,
			prepared.MerklePaymentsAddress,
		)
		if err != nil {
			return fmt.Errorf("EVM merkle payment failed: %w", err)
		}

		slog.Info("EVM merkle payment submitted",
			"uuid", upload.UUID, "winner_pool_hash", winnerHash, "total_paid", totalPaid)

		// Phase 3: Finalize merkle upload
		result, err = w.antdClient.FinalizeMerkleUpload(ctx, prepared.UploadID, winnerHash, false)
		if err != nil {
			return fmt.Errorf("Failed to finalize merkle upload: %w", err)
		}
		paidAmount = totalPaid
		txHash = winnerHash

	default: // "wave_batch" or empty (backward compat)
		// Phase 2: Sign wave-batch payment
		txHashes, err := w.evmSigner.PayForQuotes(ctx, walletKey, prepared.Payments, prepared.PaymentTokenAddress, prepared.DataPaymentsAddress)
		if err != nil {
			return fmt.Errorf("EVM payment failed: %w", err)
		}

		slog.Info("EVM payment submitted", "uuid", upload.UUID, "tx_count", len(txHashes), "total", prepared.TotalAmount)

		// Phase 3: Finalize wave-batch upload
		result, err = w.antdClient.FinalizeUpload(ctx, prepared.UploadID, txHashes, false)
		if err != nil {
			return fmt.Errorf("Failed to finalize upload: %w", err)
		}
		paidAmount = prepared.TotalAmount
		for _, h := range txHashes {
			txHash = h
			break
		}
	}

	// Mark upload completed — store the DataMap locally
	if err := w.uploadSvc.MarkCompleted(upload.ID, result.DataMap, paidAmount); err != nil {
		return fmt.Errorf("Failed to save upload record")
	}

	// Update wallet balance from chain (best-effort)
	if tokenBal, gasBal, err := w.evmSigner.GetBalances(ctx, wallet.Address, prepared.PaymentTokenAddress); err == nil {
		_ = w.walletSvc.UpdateBalance(wallet.ID, tokenBal, gasBal)
		_, _ = w.txnSvc.Record(wallet.ID, &upload.ID, "upload", paidAmount, tokenBal, txHash)
	} else {
		slog.Warn("failed to query post-payment balance", "error", err)
		_, _ = w.txnSvc.Record(wallet.ID, &upload.ID, "upload", paidAmount, wallet.PaymentBalance, txHash)
	}

	slog.Info("upload completed", "uuid", upload.UUID, "payment_type", prepared.PaymentType,
		"cost", paidAmount, "chunks", result.ChunksStored)
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
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requeued, err := w.uploadSvc.RequeueStuck(1) // stuck > 1 minute
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

// S4: tempGCLoop periodically removes orphaned temp files.
func (w *UploadWorker) tempGCLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.cleanOrphanedTempFiles()
		}
	}
}

func (w *UploadWorker) cleanOrphanedTempFiles() {
	tempDir := TempUploadDir(w.cfg)
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return
	}

	// Get all active temp paths from DB
	activePaths, err := w.uploadSvc.ListActiveTempPaths()
	if err != nil {
		slog.Warn("temp GC: failed to list active paths", "error", err)
		return
	}
	activeSet := make(map[string]struct{}, len(activePaths))
	for _, p := range activePaths {
		activeSet[p] = struct{}{}
	}

	var cleaned int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(tempDir, entry.Name())
		if _, active := activeSet[fullPath]; !active {
			// Check file age — only clean files older than 10 minutes
			// to avoid racing with in-progress uploads
			info, err := entry.Info()
			if err != nil || time.Since(info.ModTime()) < 10*time.Minute {
				continue
			}
			if err := os.Remove(fullPath); err == nil {
				cleaned++
			}
		}
	}
	if cleaned > 0 {
		slog.Info("temp GC: cleaned orphaned files", "count", cleaned)
	}
}

// TempUploadDir returns the path to the temp upload directory, creating it if needed.
func TempUploadDir(cfg *config.Config) string {
	dir := filepath.Join(cfg.DataDir, "uploads", "tmp")
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Warn("failed to create temp upload dir", "path", dir, "error", err)
	}
	return dir
}
