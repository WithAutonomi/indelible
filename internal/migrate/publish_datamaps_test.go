package migrate

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	sdk "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

// fakePublisher implements ChunkPublisher with deterministic content-addressing.
// Tracks how many times each method was called.
type fakePublisher struct {
	mu              sync.Mutex
	stored          map[string][]byte // address → bytes
	prepareCalls    int
	finalizeCalls   int
	getCalls        int
	alreadyStoredOn map[string]bool // hex address → return AlreadyStored=true on prepare
}

func newFake() *fakePublisher {
	return &fakePublisher{stored: map[string][]byte{}, alreadyStoredOn: map[string]bool{}}
}

// fakeAddress returns a deterministic 32-byte hex from the content. Mirrors the
// "content addressing" semantics without using BLAKE3.
func fakeAddress(content []byte) string {
	h := hex.EncodeToString(content)
	if len(h) >= 64 {
		return h[:64]
	}
	return h + strings.Repeat("0", 64-len(h))
}

func (f *fakePublisher) PrepareChunkUpload(_ context.Context, content []byte) (*sdk.PrepareChunkResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCalls++
	addr := fakeAddress(content)
	if f.alreadyStoredOn[addr] {
		return &sdk.PrepareChunkResult{Address: addr, AlreadyStored: true}, nil
	}
	// Use the per-call counter for unique upload_id and quote_hash even when
	// content prefixes collide (which they often do for short test payloads).
	uid := fmt.Sprintf("upload-%d", f.prepareCalls)
	qh := fmt.Sprintf("qh-%d", f.prepareCalls)
	return &sdk.PrepareChunkResult{
		Address:             addr,
		AlreadyStored:       false,
		UploadID:            uid,
		PaymentType:         "wave_batch",
		Payments:            []sdk.PaymentInfo{{QuoteHash: qh, RewardsAddress: "ra1", Amount: "100"}},
		TotalAmount:         "100",
		PaymentVaultAddress: "0xvault",
		PaymentTokenAddress: "0xtoken",
		RPCUrl:              "http://test/rpc",
	}, nil
}

func (f *fakePublisher) FinalizeChunkUpload(_ context.Context, uploadID string, _ map[string]string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finalizeCalls++
	addr := strings.TrimPrefix(uploadID, "upload-")
	addr += strings.Repeat("0", 64-len(addr))
	// Note: the migrator only uses the returned address; we don't need to map
	// back to the original bytes here. Real antd guarantees address-match
	// against prepare; the migrator enforces that invariant itself.
	return addr, nil
}

func (f *fakePublisher) ChunkGet(_ context.Context, address string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	data, ok := f.stored[address]
	if !ok {
		return nil, fmt.Errorf("not found: %s", address)
	}
	return data, nil
}

// happyPublisher is fakePublisher with a wired prepare/finalize chain that
// returns matching addresses so the migrator's address-match check passes.
// Keeps an explicit upload_id → address map (real antd stashes the prepared
// chunk by upload_id internally; emulating that gives us correct semantics
// even when content prefixes collide in our deterministic fake addresses).
type happyPublisher struct {
	*fakePublisher
	uploadAddrs map[string]string // upload_id → address
}

func newHappy() *happyPublisher {
	return &happyPublisher{fakePublisher: newFake(), uploadAddrs: map[string]string{}}
}

func (h *happyPublisher) PrepareChunkUpload(ctx context.Context, content []byte) (*sdk.PrepareChunkResult, error) {
	res, err := h.fakePublisher.PrepareChunkUpload(ctx, content)
	if err != nil || res.AlreadyStored {
		return res, err
	}
	h.mu.Lock()
	cp := make([]byte, len(content))
	copy(cp, content)
	h.stored[res.Address] = cp
	h.uploadAddrs[res.UploadID] = res.Address
	h.mu.Unlock()
	return res, nil
}

func (h *happyPublisher) FinalizeChunkUpload(_ context.Context, uploadID string, _ map[string]string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finalizeCalls++
	addr, ok := h.uploadAddrs[uploadID]
	if !ok {
		return "", fmt.Errorf("upload_id %s not found", uploadID)
	}
	return addr, nil
}

// fakePayer implements EvmPayer; returns one tx hash per payment.
type fakePayer struct {
	mu    sync.Mutex
	calls int
}

func (p *fakePayer) PayForQuotes(_ context.Context, _ string, payments []sdk.PaymentInfo,
	_ string, _ string) (map[string]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	out := map[string]string{}
	for _, pay := range payments {
		out[pay.QuoteHash] = "tx-" + pay.QuoteHash
	}
	return out, nil
}

func setupDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newUser(t *testing.T, db *database.DB, email string) int64 {
	t.Helper()
	u, err := services.NewUserService(db).Create(email, "hashed_pw", "T", "U")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	return u.ID
}

// seedCompletedPrivate creates an upload row, transitions to completed with a
// hex-encoded data_map blob. Returns the row's UUID.
func seedCompletedPrivate(t *testing.T, svc *services.UploadService, userID int64, name string, payload []byte) string {
	t.Helper()
	u, err := svc.Create(userID, nil, name, name, int64(len(payload)),
		"application/octet-stream", "private", "/tmp/"+name, nil)
	if err != nil {
		t.Fatalf("seed create %s: %v", name, err)
	}
	if err := svc.MarkCompleted(u.ID, hex.EncodeToString(payload), "100"); err != nil {
		t.Fatalf("seed mark-completed %s: %v", name, err)
	}
	return u.UUID
}

func TestPublishDataMaps_HappyPath(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "happy@example.com")
	svc := services.NewUploadService(db)

	wantUUIDs := []string{
		seedCompletedPrivate(t, svc, uid, "a.pdf", []byte("alpha-datamap")),
		seedCompletedPrivate(t, svc, uid, "b.pdf", []byte("bravo-datamap")),
	}

	pub := newHappy()
	pay := &fakePayer{}

	var buf bytes.Buffer
	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef",
		Options{}, &buf)
	if err != nil {
		t.Fatalf("PublishDataMaps: %v", err)
	}

	if run.Candidates != 2 || run.Published != 2 || run.Failed != 0 {
		t.Errorf("summary cand=%d pub=%d fail=%d, want 2/2/0",
			run.Candidates, run.Published, run.Failed)
	}
	if pub.prepareCalls != 2 || pay.calls != 2 || pub.finalizeCalls != 2 {
		t.Errorf("call counts: prepare=%d pay=%d finalize=%d, want 2/2/2",
			pub.prepareCalls, pay.calls, pub.finalizeCalls)
	}

	for _, uuid := range wantUUIDs {
		got, _ := svc.GetByUUID(uuid)
		if got.Visibility != "public" || !got.DatamapAddress.Valid {
			t.Errorf("%s not flipped: visibility=%s, datamap_address.valid=%v",
				uuid, got.Visibility, got.DatamapAddress.Valid)
		}
	}

	if strings.Count(buf.String(), `"outcome":"published"`) != 2 {
		t.Errorf("expected 2 published outcomes in progress, got: %s", buf.String())
	}
}

func TestPublishDataMaps_AlreadyStoredSkipsPayment(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "as@example.com")
	svc := services.NewUploadService(db)
	payload := []byte("already-on-network")
	uuid := seedCompletedPrivate(t, svc, uid, "x.pdf", payload)

	pub := newHappy()
	pub.alreadyStoredOn[fakeAddress(payload)] = true
	pay := &fakePayer{}

	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef", Options{}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.AlreadyStored != 1 || run.Published != 1 || pay.calls != 0 || pub.finalizeCalls != 0 {
		t.Errorf("expected already_stored=1, no pay/finalize calls. got AS=%d pub=%d pay=%d fin=%d",
			run.AlreadyStored, run.Published, pay.calls, pub.finalizeCalls)
	}
	got, _ := svc.GetByUUID(uuid)
	if got.Visibility != "public" || !got.DatamapAddress.Valid {
		t.Errorf("row not flipped after already_stored")
	}
}

func TestPublishDataMaps_DryRunMakesNoChanges(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "dry@example.com")
	svc := services.NewUploadService(db)
	uuid := seedCompletedPrivate(t, svc, uid, "a.pdf", []byte("p"))

	pub := newHappy()
	pay := &fakePayer{}

	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef",
		Options{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Candidates != 1 || run.Published != 0 {
		t.Errorf("dry-run: cand=%d pub=%d, want 1/0", run.Candidates, run.Published)
	}
	if pub.prepareCalls != 0 || pay.calls != 0 {
		t.Errorf("dry-run still made network calls: prepare=%d pay=%d", pub.prepareCalls, pay.calls)
	}
	got, _ := svc.GetByUUID(uuid)
	if got.Visibility != "private" || got.DatamapAddress.Valid {
		t.Errorf("dry-run mutated row")
	}
}

// failingPublisher fails the first PrepareChunkUpload, then delegates.
type failingPublisher struct {
	inner     *happyPublisher
	failFirst error
	calls     int
}

func (f *failingPublisher) PrepareChunkUpload(ctx context.Context, content []byte) (*sdk.PrepareChunkResult, error) {
	f.calls++
	if f.calls == 1 && f.failFirst != nil {
		return nil, f.failFirst
	}
	return f.inner.PrepareChunkUpload(ctx, content)
}
func (f *failingPublisher) FinalizeChunkUpload(ctx context.Context, uploadID string, txHashes map[string]string) (string, error) {
	return f.inner.FinalizeChunkUpload(ctx, uploadID, txHashes)
}
func (f *failingPublisher) ChunkGet(ctx context.Context, address string) ([]byte, error) {
	return f.inner.ChunkGet(ctx, address)
}

func TestPublishDataMaps_PrepareErrorContinues(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "err@example.com")
	svc := services.NewUploadService(db)
	seedCompletedPrivate(t, svc, uid, "a.pdf", []byte("alpha"))
	seedCompletedPrivate(t, svc, uid, "b.pdf", []byte("bravo"))

	pub := &failingPublisher{inner: newHappy(), failFirst: errors.New("transient network blip")}
	pay := &fakePayer{}

	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef", Options{}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Failed != 1 || run.Published != 1 {
		t.Errorf("expected failed=1 published=1, got failed=%d published=%d", run.Failed, run.Published)
	}
}

// corruptingPublisher returns wrong bytes on ChunkGet — for verify-mismatch coverage.
type corruptingPublisher struct{ *happyPublisher }

func (c *corruptingPublisher) ChunkGet(ctx context.Context, address string) ([]byte, error) {
	b, err := c.happyPublisher.ChunkGet(ctx, address)
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, b...)
	out[0] ^= 0xff
	return out, nil
}

func TestPublishDataMaps_VerifyMismatchKeepsRowPrivate(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "v@example.com")
	svc := services.NewUploadService(db)
	uuid := seedCompletedPrivate(t, svc, uid, "x.pdf", []byte("real datamap"))

	pub := &corruptingPublisher{happyPublisher: newHappy()}
	pay := &fakePayer{}

	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef",
		Options{Verify: true}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Failed != 1 || run.Verified != 0 {
		t.Errorf("expected failed=1 verified=0, got failed=%d verified=%d", run.Failed, run.Verified)
	}
	got, _ := svc.GetByUUID(uuid)
	if got.Visibility != "private" {
		t.Errorf("verify failure still flipped row to %s", got.Visibility)
	}
}

func TestPublishDataMaps_LimitAndUUIDFilter(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "f@example.com")
	svc := services.NewUploadService(db)
	for i := 0; i < 5; i++ {
		seedCompletedPrivate(t, svc, uid, fmt.Sprintf("f%d.pdf", i), []byte(fmt.Sprintf("payload-%d", i)))
	}

	pub := newHappy()
	pay := &fakePayer{}

	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef",
		Options{Limit: 2}, nil)
	if err != nil || run.Published != 2 {
		t.Fatalf("limit=2: err=%v published=%d", err, run.Published)
	}

	remaining, _ := svc.ListPrivatePublishCandidates(0)
	if len(remaining) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(remaining))
	}
	target := remaining[1].UUID

	pub2 := newHappy()
	pay2 := &fakePayer{}
	run, err = PublishDataMaps(context.Background(), svc, pub2, pay2, "deadbeef",
		Options{UUID: target}, nil)
	if err != nil || run.Published != 1 || run.Candidates != 1 {
		t.Fatalf("uuid filter: err=%v pub=%d cand=%d", err, run.Published, run.Candidates)
	}
	got, _ := svc.GetByUUID(target)
	if got.Visibility != "public" {
		t.Errorf("uuid-filter target still private")
	}
}

func TestPublishDataMaps_SkipsBadHex(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "bad@example.com")
	svc := services.NewUploadService(db)
	uuid := seedCompletedPrivate(t, svc, uid, "x.pdf", []byte("ok"))
	got, _ := svc.GetByUUID(uuid)
	if _, err := db.Exec(`UPDATE uploads SET data_map = 'zzz-not-hex' WHERE id = ?`, got.ID); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	pub := newHappy()
	pay := &fakePayer{}
	run, err := PublishDataMaps(context.Background(), svc, pub, pay, "deadbeef", Options{}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Failed != 1 || run.Published != 0 || pub.prepareCalls != 0 {
		t.Errorf("bad-hex: failed=%d pub=%d prepareCalls=%d, want 1/0/0",
			run.Failed, run.Published, pub.prepareCalls)
	}
}
