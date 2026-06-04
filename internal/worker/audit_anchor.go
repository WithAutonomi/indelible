package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/evm"
	"github.com/WithAutonomi/indelible/internal/migrate"
	"github.com/WithAutonomi/indelible/internal/services"
)

const (
	// auditAnchorEnabledSetting gates anchoring — opt-in (off by default) because
	// each anchor is a paid Autonomi write (ANT + gas).
	auditAnchorEnabledSetting  = "audit_anchor_enabled"
	auditAnchorIntervalSetting = "audit_anchor_interval_hours"
	defaultAuditAnchorInterval = 24 * time.Hour
)

// AuditAnchorWorker periodically commits the audit-log hash-chain head to
// Autonomi (V2-453), making the chain externally verifiable. It is cost-gated:
// disabled by default, and skipped when no default wallet / EVM RPC is available.
type AuditAnchorWorker struct {
	logSvc      *services.LogService
	walletSvc   *services.WalletService
	settingsSvc *services.SettingsService
	publisher   migrate.ChunkPublisher
	cfg         *config.Config

	// newPayer builds the EVM payer for an RPC URL. Overridable in tests so the
	// anchor flow can run against fakes with no network.
	newPayer func(rpcURL string) (migrate.EvmPayer, error)

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewAuditAnchorWorker creates the anchor worker with real antd + EVM payment.
func NewAuditAnchorWorker(db *database.DB, cfg *config.Config) *AuditAnchorWorker {
	return &AuditAnchorWorker{
		logSvc:      services.NewLogService(db),
		walletSvc:   services.NewWalletService(db, cfg.WalletEncryptionKey),
		settingsSvc: services.NewSettingsService(db),
		publisher:   antd.NewClient(cfg.AntdURL, antd.WithTimeout(0)),
		cfg:         cfg,
		newPayer:    func(rpcURL string) (migrate.EvmPayer, error) { return evm.NewSigner(rpcURL) },
	}
}

// Start begins the periodic anchoring loop.
func (w *AuditAnchorWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.loop(ctx)
	}()

	slog.Info("audit anchor worker started")
}

// Stop gracefully shuts down the worker.
func (w *AuditAnchorWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

func (w *AuditAnchorWorker) interval() time.Duration {
	if v, err := w.settingsSvc.Get(auditAnchorIntervalSetting); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}
	return defaultAuditAnchorInterval
}

func (w *AuditAnchorWorker) loop(ctx context.Context) {
	ticker := time.NewTicker(w.interval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.anchorOnce(ctx); err != nil {
				slog.Error("audit anchor failed", "error", err)
			}
		}
	}
}

// anchorOnce performs a single anchoring pass. It returns (true, nil) when an
// anchor was written, (false, nil) when anchoring was skipped (disabled, no
// wallet, no new rows, no RPC), and (false, err) on a real failure. Exported
// surface is intentionally narrow; this is the unit-tested core.
func (w *AuditAnchorWorker) anchorOnce(ctx context.Context) (bool, error) {
	// Opt-in: each anchor costs ANT + gas, so it is off unless explicitly enabled.
	if v, _ := w.settingsSvc.Get(auditAnchorEnabledSetting); v != "true" {
		return false, nil
	}

	wallet, err := w.walletSvc.GetDefault()
	if err != nil || wallet == nil {
		slog.Debug("audit anchor skipped: no default wallet")
		return false, nil
	}

	headHash, count, err := w.logSvc.AuditChainHead()
	if err != nil {
		return false, err
	}
	if headHash == "" {
		return false, nil // nothing chained yet
	}

	// Skip when the current head is already anchored (no new audit rows since).
	if latest, _ := w.logSvc.LatestAuditAnchor(); latest != nil && latest.RowCount == count && latest.HeadHash == headHash {
		return false, nil
	}

	rpcURL := w.cfg.EvmRPCURL
	if rpcURL == "" {
		slog.Warn("audit anchor skipped: EVM RPC not configured yet")
		return false, nil
	}

	walletKey, err := w.walletSvc.DecryptKey(wallet)
	if err != nil {
		return false, fmt.Errorf("decrypt wallet key: %w", err)
	}
	payer, err := w.newPayer(rpcURL)
	if err != nil {
		return false, fmt.Errorf("build payer: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"kind":      "indelible-audit-anchor",
		"head_hash": headHash,
		"row_count": count,
	})
	if err != nil {
		return false, err
	}

	// verify=true: round-trips ChunkGet so we only record an anchor we confirmed
	// is retrievable from the network.
	addr, _, err := migrate.PublishChunk(ctx, w.publisher, payer, walletKey, payload, true)
	if err != nil {
		return false, fmt.Errorf("publish anchor: %w", err)
	}

	if err := w.logSvc.RecordAuditAnchor(headHash, count, addr, ""); err != nil {
		return false, fmt.Errorf("record anchor: %w", err)
	}

	slog.Info("audit chain anchored to Autonomi", "row_count", count, "address", addr)
	return true, nil
}
