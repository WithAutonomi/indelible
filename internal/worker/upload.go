package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
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

// errFinalizeFailed marks a wave-batch finalize failure. Retrying re-Prepares,
// which returns already-stored chunks at zero cost (content-addressed dedup), so
// re-driving finalize is safe and cannot double-charge.
var errFinalizeFailed = errors.New("finalize failed")

// errPaidNoRetry marks a failure where a payment was made and re-running the
// whole upload could pay again (the merkle path always submits a payment; its
// re-payment is not provably zero-cost). The source is preserved for manual /
// future reconciliation instead.
var errPaidNoRetry = errors.New("paid, cannot safely retry")

// uploadOutcome is how processOne should react to a failed processUpload attempt.
type uploadOutcome int

const (
	outcomeRetry               uploadOutcome = iota // attempts remain and re-running is safe
	outcomeAbandon                                  // nothing paid → mark failed and delete temp
	outcomePreservePaid                             // payment confirmed but upload unfinished → keep temp for recovery
	outcomePreserveUnconfirmed                      // tx broadcast but unconfirmed → keep temp, must NOT re-pay
)

// classifyFailure decides the outcome for a failed attempt. paymentMade reflects
// whether any payment has been recorded for this upload (crash/retry-safe);
// canRetry reflects whether more attempts remain. The ordering is deliberate:
// the two "don't re-pay" cases (unconfirmed tx, merkle-paid) win over any retry,
// and a generic transient error is only retried while nothing has been paid.
func classifyFailure(err error, paymentMade, canRetry bool) uploadOutcome {
	switch {
	case errors.Is(err, evm.ErrConfirmationTimeout):
		return outcomePreserveUnconfirmed
	case errors.Is(err, errPaidNoRetry):
		return outcomePreservePaid
	case errors.Is(err, errFinalizeFailed) && canRetry:
		return outcomeRetry
	case isTransientAntdError(err) && canRetry && !paymentMade:
		return outcomeRetry
	case paymentMade:
		return outcomePreservePaid
	default:
		return outcomeAbandon
	}
}

// estimatedUploadCost returns the cost to compare against max_gas_fee. For
// wave-batch it's the quoted TotalAmount; for merkle (cost determined on-chain)
// it's the upper bound the contract could charge — the sum over pools of the
// largest candidate amount. Unparseable/empty amounts count as zero.
func estimatedUploadCost(prepared *antd.PrepareUploadResult) *big.Int {
	if prepared.PaymentType == "merkle" {
		total := new(big.Int)
		for _, pc := range prepared.PoolCommitments {
			poolMax := new(big.Int)
			for _, c := range pc.Candidates {
				if amt, ok := new(big.Int).SetString(c.Amount, 10); ok && amt.Cmp(poolMax) > 0 {
					poolMax = amt
				}
			}
			total.Add(total, poolMax)
		}
		return total
	}
	if amt, ok := new(big.Int).SetString(strings.TrimSpace(prepared.TotalAmount), 10); ok {
		return amt
	}
	return new(big.Int)
}

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
func NewUploadWorker(db *database.DB, cfg *config.Config) *UploadWorker {
	return &UploadWorker{
		uploadSvc:       services.NewUploadService(db),
		quotaSvc:        services.NewQuotaService(db),
		txnSvc:          services.NewTransactionService(db),
		walletSvc:       services.NewWalletService(db, cfg.WalletEncryptionKey),
		webhookSvc:      services.NewWebhookDeliveryService(db),
		settingsSvc:     services.NewCachedSettingsService(services.NewSettingsService(db)),
		antdClient:      antd.NewClient(cfg.AntdURL, antd.WithTimeout(0)),
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

	for attempt := 0; attempt <= maxTransientRetries; attempt++ {
		err := w.processUpload(ctx, upload)
		if err == nil {
			w.prepareFailures = 0
			w.circuitCooldown = circuitBreakerBaseCooldown
			// processUpload set upload.Status to "completed" or, for a
			// content-addressed dedup (V2-399), "already_stored". Fire the
			// success event with whichever status it landed on.
			if upload.Status != "completed" && upload.Status != "already_stored" {
				upload.Status = "completed"
			}
			w.webhookSvc.FireUploadEvent("completed", upload)
			w.cleanupTempFile(upload)
			return
		}

		if errors.Is(err, errGasBackoff) {
			slog.Warn("upload gas backoff", "uuid", upload.UUID, "attempt", upload.BackoffAttempt+1)
			return
		}

		paymentMade, _ := w.txnSvc.HasByUpload(upload.ID)
		outcome := classifyFailure(err, paymentMade, attempt < maxTransientRetries)
		if outcome == outcomeRetry {
			delay := time.Duration(attempt+1) * 5 * time.Second
			slog.Warn("retrying upload", "uuid", upload.UUID, "attempt", attempt+1, "delay", delay, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			continue
		}

		// Terminal outcome — stop retrying and resolve the failure.
		w.finishFailedUpload(upload, err, outcome)
		return
	}
}

// finishFailedUpload applies the terminal outcome for an upload that will not be
// retried. The no-payment case is abandoned (mark failed + delete the temp
// source); when money or chunks may be committed the temp source is preserved
// with a recoverable status_detail, so the DataMap can still be recovered and the
// spend isn't lost (V2-425 / V2-426).
func (w *UploadWorker) finishFailedUpload(upload *services.Upload, err error, outcome uploadOutcome) {
	switch outcome {
	case outcomePreserveUnconfirmed:
		slog.Error("payment unconfirmed — preserving source for reconciliation, not re-paying",
			"uuid", upload.UUID, "error", err)
		_ = w.uploadSvc.MarkFailedPreserveTemp(upload.ID, err.Error(), services.StatusDetailPaymentUnconfirmed)
	case outcomePreservePaid:
		slog.Error("payment made but upload not finalized — preserving source for recovery",
			"uuid", upload.UUID, "error", err)
		_ = w.uploadSvc.MarkFailedPreserveTemp(upload.ID, err.Error(), services.StatusDetailPaidUnfinalized)
	default: // outcomeAbandon — nothing was paid, so the source is safe to delete
		// S9: track consecutive transient failures with exponential cooldown.
		if isTransientAntdError(err) {
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
		slog.Error("upload failed", "uuid", upload.UUID, "error", err)
		_ = w.uploadSvc.MarkFailed(upload.ID, err.Error())
		w.cleanupTempFile(upload)
	}
	upload.Status = "failed"
	w.webhookSvc.FireUploadEvent("failed", upload)
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

	// S3: Re-check quota at processing time (includes in-flight bytes). Pass
	// the upload's token_id so the department tier can resolve correctly.
	var tID *int64
	if upload.TokenID.Valid {
		v := upload.TokenID.Int64
		tID = &v
	}
	if err := w.quotaSvc.CheckUserQuotaInFlight(upload.UserID, tID, upload.FileSize); err != nil {
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

	// Phase 1: Prepare upload — encrypts file, collects network quotes.
	// Public visibility: daemon bundles the serialized DataMap chunk into the
	// same payment batch so the external signer pays for chunks + DataMap in
	// one EVM tx, and finalize returns a network address for the DataMap.
	// Private visibility: DataMap stays in-memory and is stored locally.
	var prepared *antd.PrepareUploadResult
	if upload.Visibility == "public" {
		prepared, err = w.antdClient.PrepareUploadPublic(ctx, tempPath)
	} else {
		prepared, err = w.antdClient.PrepareUpload(ctx, tempPath)
	}
	if err != nil {
		return fmt.Errorf("Failed to prepare upload: %w", err)
	}

	// Cost ceiling — applies to wave-batch AND merkle. Wave cost is known upfront
	// (prepared.TotalAmount); merkle cost is the most the contract could charge
	// (one winning candidate per pool). Either exceeding max_gas_fee backs off to
	// a cheaper window rather than paying uncapped. Compared as big.Int so large
	// atto-token amounts don't overflow.
	if maxFeeStr, err := w.settingsSvc.Get("max_gas_fee"); err == nil {
		if maxFee, ok := new(big.Int).SetString(strings.TrimSpace(maxFeeStr), 10); ok && maxFee.Sign() > 0 {
			estCost := estimatedUploadCost(prepared)
			if estCost.Cmp(maxFee) > 0 {
				attempt := upload.BackoffAttempt + 1
				if attempt > maxGasBackoffAttempts {
					return fmt.Errorf("Gas fees too high — try again later")
				}
				backoffUntil := calcGasBackoff(attempt)
				if err := w.uploadSvc.SetGasBackoff(upload.ID, backoffUntil, attempt, estCost.String()); err != nil {
					return fmt.Errorf("Internal error scheduling retry")
				}
				slog.Info("cost too high, backing off",
					"uuid", upload.UUID, "quoted", estCost.String(), "max", maxFeeStr, "payment_type", prepared.PaymentType,
					"attempt", attempt, "retry_at", backoffUntil.Format(time.RFC3339))
				return errGasBackoff
			}
			if upload.BackoffAttempt > 0 {
				_ = w.uploadSvc.ClearBackoff(upload.ID)
				slog.Info("cost acceptable after backoff", "uuid", upload.UUID, "quoted", estCost.String(), "attempts", upload.BackoffAttempt)
			}
			slog.Info("cost check passed", "uuid", upload.UUID, "quoted", estCost.String(), "max", maxFeeStr, "payment_type", prepared.PaymentType)
		}
	}

	// Resolve effective RPC URL and token address. We prefer indelible's own
	// config when it's set (populated by --network or INDELIBLE_EVM_* env vars)
	// and only fall back to antd's PrepareUpload response when we have nothing
	// else. Without this, a misconfigured antd (e.g. one that defaults
	// EVM_RPC_URL to http://127.0.0.1:8545 when env isn't set) silently
	// redirects payment traffic to a dead/wrong endpoint even though we know
	// the right one locally.
	rpcURL := w.cfg.EvmRPCURL
	if rpcURL == "" {
		rpcURL = prepared.RPCUrl
	}
	tokenAddr := w.cfg.EvmTokenAddress
	if tokenAddr == "" {
		tokenAddr = prepared.PaymentTokenAddress
	}
	if w.cfg.EvmRPCURL != "" && prepared.RPCUrl != "" && prepared.RPCUrl != w.cfg.EvmRPCURL {
		slog.Warn("antd returned a different EVM RPC URL than configured — using config",
			"configured", w.cfg.EvmRPCURL, "antd_returned", prepared.RPCUrl)
	}

	// Cache antd's response only when our config is empty — preserves the
	// original "first PrepareUpload populates cfg" behaviour for installs
	// that rely on antd as authority.
	if w.cfg.EvmRPCURL == "" && prepared.RPCUrl != "" {
		w.cfg.EvmRPCURL = prepared.RPCUrl
		w.cfg.EvmTokenAddress = prepared.PaymentTokenAddress
	}

	// Ensure EVM signer is connected to the resolved URL.
	if w.evmSigner == nil || w.evmSigner.RPCUrl() != rpcURL {
		signer, err := evm.NewSigner(rpcURL)
		if err != nil {
			return fmt.Errorf("Failed to connect to EVM RPC: %w", err)
		}
		w.evmSigner = signer
	}

	// Optional operator override for how long we wait for a payment tx to
	// confirm before freeing the worker slot (defaults to the signer's built-in
	// bound). Read each time so it can be tuned without a restart.
	if v, err := w.settingsSvc.Get("payment_confirmation_timeout_seconds"); err == nil {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
			w.evmSigner.SetConfirmationTimeout(time.Duration(secs) * time.Second)
		}
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
			tokenAddr,
			prepared.PaymentVaultAddress,
		)
		if err != nil {
			return fmt.Errorf("EVM merkle payment failed: %w", err)
		}
		paidAmount = totalPaid
		txHash = winnerHash

		slog.Info("EVM merkle payment submitted",
			"uuid", upload.UUID, "winner_pool_hash", winnerHash, "total_paid", totalPaid)

		// Record the confirmed spend BEFORE finalize, so a finalize failure still
		// leaves an accounting record rather than losing the payment (V2-426).
		w.recordPayment(ctx, wallet, upload, tokenAddr, paidAmount, txHash)

		// Phase 3: Finalize merkle upload. A failure here means money is already
		// spent; re-running would submit a second merkle payment (not provably
		// zero-cost), so flag it no-retry and preserve the source for recovery.
		result, err = w.antdClient.FinalizeMerkleUpload(ctx, prepared.UploadID, winnerHash, false)
		if err != nil {
			return fmt.Errorf("Failed to finalize merkle upload: %w", errors.Join(errPaidNoRetry, err))
		}

	default: // "wave_batch" or empty (backward compat)
		// Phase 2: Sign wave-batch payment. When prepare returns no quotes,
		// every chunk is already on-network (content-addressed dedup) — there's
		// nothing to pay, so skip signing an empty batch and finalize directly.
		var txHashes map[string]string
		paymentMade := false
		if len(prepared.Payments) > 0 {
			txHashes, err = w.evmSigner.PayForQuotes(ctx, walletKey, prepared.Payments, tokenAddr, prepared.PaymentVaultAddress)
			if err != nil {
				return fmt.Errorf("EVM payment failed: %w", err)
			}
			paymentMade = true
			slog.Info("EVM payment submitted", "uuid", upload.UUID, "tx_count", len(txHashes), "total", prepared.TotalAmount)
		} else {
			slog.Info("no quotes to pay — chunks already stored", "uuid", upload.UUID)
		}
		paidAmount = prepared.TotalAmount
		for _, h := range txHashes {
			txHash = h
			break
		}

		// Record the confirmed spend BEFORE finalize (V2-426). Only when a payment
		// actually happened — a dedup re-Prepare pays nothing.
		if paymentMade {
			w.recordPayment(ctx, wallet, upload, tokenAddr, paidAmount, txHash)
		}

		// Phase 3: Finalize wave-batch upload. Retrying re-Prepares at zero cost
		// (dedup), so a finalize failure is safe to retry.
		result, err = w.antdClient.FinalizeUpload(ctx, prepared.UploadID, txHashes, false)
		if err != nil {
			return fmt.Errorf("Failed to finalize upload: %w", errors.Join(errFinalizeFailed, err))
		}
	}

	// Mark upload completed. Public uploads have a published DataMap address
	// (the bundled DataMap chunk's on-network address); private uploads carry
	// the raw hex-encoded DataMap stored locally.
	//
	// ChunksStored == 0 means every chunk was already on the network — a
	// content-addressed dedup (idempotent re-upload). Record it as
	// "already_stored" so the UI reads "already on network" rather than a fresh
	// store; nothing was paid (empty quote batch). (V2-399)
	alreadyStored := result.ChunksStored == 0
	if upload.Visibility == "public" {
		if result.DataMapAddress == "" {
			return fmt.Errorf("Daemon did not return data_map_address for public upload — antd >= 0.6.1 required")
		}
		if alreadyStored {
			upload.Status = "already_stored"
			if err := w.uploadSvc.MarkAlreadyStoredPublic(upload.ID, result.DataMapAddress, paidAmount); err != nil {
				return fmt.Errorf("Failed to save upload record")
			}
		} else {
			upload.Status = "completed"
			if err := w.uploadSvc.MarkCompletedPublic(upload.ID, result.DataMapAddress, paidAmount); err != nil {
				return fmt.Errorf("Failed to save upload record")
			}
		}
	} else {
		if alreadyStored {
			upload.Status = "already_stored"
			if err := w.uploadSvc.MarkAlreadyStored(upload.ID, result.DataMap, paidAmount); err != nil {
				return fmt.Errorf("Failed to save upload record")
			}
		} else {
			upload.Status = "completed"
			if err := w.uploadSvc.MarkCompleted(upload.ID, result.DataMap, paidAmount); err != nil {
				return fmt.Errorf("Failed to save upload record")
			}
		}
	}

	slog.Info("upload completed", "uuid", upload.UUID, "payment_type", prepared.PaymentType,
		"cost", paidAmount, "chunks", result.ChunksStored)
	return nil
}

// recordPayment persists the wallet spend (and refreshes the cached balance) as
// soon as a payment is confirmed — before finalize — so a later finalize failure
// still leaves a queryable accounting record rather than losing the spend. Called
// exactly once per real payment (a dedup re-Prepare pays nothing, so retries do
// not double-record).
func (w *UploadWorker) recordPayment(ctx context.Context, wallet *services.Wallet, upload *services.Upload, tokenAddr, paidAmount, txHash string) {
	if tokenBal, gasBal, err := w.evmSigner.GetBalances(ctx, wallet.Address, tokenAddr); err == nil {
		_ = w.walletSvc.UpdateBalance(wallet.ID, tokenBal, gasBal)
		_, _ = w.txnSvc.Record(wallet.ID, &upload.ID, "upload", paidAmount, tokenBal, txHash)
	} else {
		slog.Warn("failed to query post-payment balance", "error", err)
		_, _ = w.txnSvc.Record(wallet.ID, &upload.ID, "upload", paidAmount, wallet.PaymentBalance, txHash)
	}
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
			requeued, err := w.uploadSvc.RequeueStuck(60) // 60 min — must exceed longest realistic upload to avoid racing a live goroutine; 30 min was empirically insufficient on mainnet bootstrap
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

// TempUploadDir returns the absolute path to the temp upload directory,
// creating it if needed. The absolute form is important: antd opens upload
// files by the path we hand it via PrepareUpload, so a path that's relative
// to indelible's cwd will 400 if antd is running with a different cwd.
func TempUploadDir(cfg *config.Config) string {
	dir := filepath.Join(cfg.DataDir, "uploads", "tmp")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Warn("failed to create temp upload dir", "path", dir, "error", err)
	}
	return dir
}
