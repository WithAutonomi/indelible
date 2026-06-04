package worker

import (
	"context"
	"sync"
	"testing"

	sdk "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/migrate"
	"github.com/WithAutonomi/indelible/internal/services"
)

// rtPublisher is a round-trip-capable fake ChunkPublisher: it "stores" the
// prepared content under a fixed address and returns it from ChunkGet, so
// PublishChunk's verify step passes without a network.
type rtPublisher struct {
	mu     sync.Mutex
	stored map[string][]byte
}

func newRTPublisher() *rtPublisher { return &rtPublisher{stored: map[string][]byte{}} }

const fakeAnchorAddr = "0xanchoraddress"

func (p *rtPublisher) PrepareChunkUpload(_ context.Context, content []byte) (*sdk.PrepareChunkResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stored[fakeAnchorAddr] = append([]byte(nil), content...)
	return &sdk.PrepareChunkResult{
		Address:             fakeAnchorAddr,
		AlreadyStored:       false,
		UploadID:            "up1",
		Payments:            []sdk.PaymentInfo{{QuoteHash: "q", RewardsAddress: "r", Amount: "1"}},
		PaymentVaultAddress: "0xvault",
		PaymentTokenAddress: "0xtoken",
	}, nil
}

func (p *rtPublisher) FinalizeChunkUpload(_ context.Context, _ string, _ map[string]string) (string, error) {
	return fakeAnchorAddr, nil
}

func (p *rtPublisher) ChunkGet(_ context.Context, addr string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stored[addr], nil
}

type fakeAnchorPayer struct{}

func (fakeAnchorPayer) PayForQuotes(_ context.Context, _ string, _ []sdk.PaymentInfo, _, _ string) (map[string]string, error) {
	return map[string]string{"q": "0xtx"}, nil
}

func newTestAnchorWorker(t *testing.T) (*AuditAnchorWorker, *services.LogService) {
	t.Helper()
	db := dbtest.OpenDB(t)
	cfg := &config.Config{
		WalletEncryptionKey: "1111111111111111111111111111111111111111111111111111111111111111",
		EvmRPCURL:           "http://localhost:8545",
	}
	logSvc := services.NewLogService(db)
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)
	if _, err := walletSvc.Create("test", "0xabc", "deadbeef"); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('audit_anchor_enabled', 'true')`); err != nil {
		t.Fatalf("enable anchoring: %v", err)
	}
	w := &AuditAnchorWorker{
		logSvc:      logSvc,
		walletSvc:   walletSvc,
		settingsSvc: services.NewSettingsService(db),
		publisher:   newRTPublisher(),
		cfg:         cfg,
		newPayer:    func(string) (migrate.EvmPayer, error) { return fakeAnchorPayer{}, nil },
	}
	return w, logSvc
}

func TestAuditAnchorWorker_AnchorsAndSkips(t *testing.T) {
	w, logSvc := newTestAnchorWorker(t)

	// No chained rows yet → nothing to anchor.
	if anchored, err := w.anchorOnce(context.Background()); err != nil || anchored {
		t.Fatalf("empty chain: anchored=%v err=%v, want false/nil", anchored, err)
	}

	// Write three chained audit rows, then anchor.
	for range 3 {
		if err := logSvc.WriteAudit("test_event", "info", nil, "d", "", "", ""); err != nil {
			t.Fatalf("write audit: %v", err)
		}
	}
	anchored, err := w.anchorOnce(context.Background())
	if err != nil || !anchored {
		t.Fatalf("anchor: anchored=%v err=%v, want true/nil", anchored, err)
	}

	latest, err := logSvc.LatestAuditAnchor()
	if err != nil || latest == nil {
		t.Fatalf("latest anchor: %v, %v", latest, err)
	}
	if latest.RowCount != 3 || latest.NetworkAddress != fakeAnchorAddr {
		t.Errorf("anchor row_count=%d addr=%q, want 3/%q", latest.RowCount, latest.NetworkAddress, fakeAnchorAddr)
	}

	// Verify reports the chain intact AND matching the anchor.
	res, err := logSvc.VerifyAuditChain()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Intact || !res.AnchorChecked || !res.AnchorMatches {
		t.Errorf("verify result = %+v, want intact + anchor match", res)
	}

	// Re-anchoring with no new rows is a no-op.
	if anchored, err := w.anchorOnce(context.Background()); err != nil || anchored {
		t.Errorf("re-anchor no-new-rows: anchored=%v err=%v, want false/nil", anchored, err)
	}

	// Disabling the setting suppresses anchoring even with new rows.
	if err := logSvc.WriteAudit("another", "info", nil, "d", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.settingsSvc.SetInternal("audit_anchor_enabled", "false"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if anchored, err := w.anchorOnce(context.Background()); err != nil || anchored {
		t.Errorf("disabled: anchored=%v err=%v, want false/nil", anchored, err)
	}
}
