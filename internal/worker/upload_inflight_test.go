package worker

import (
	"errors"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/services"
)

func TestStuckRequeueWindowOverride(t *testing.T) {
	t.Cleanup(ResetStuckRequeueAfter)

	if d, over := StuckRequeueAfter(); d != DefaultStuckRequeueAfter || over {
		t.Fatalf("default = %s overridden=%v, want %s false", d, over, DefaultStuckRequeueAfter)
	}
	if err := SetStuckRequeueAfter(4 * time.Hour); err != nil {
		t.Fatalf("set 4h: %v", err)
	}
	if d, over := StuckRequeueAfter(); d != 4*time.Hour || !over {
		t.Fatalf("after set = %s overridden=%v, want 4h true", d, over)
	}
	for _, bad := range []time.Duration{0, 30 * time.Second, MaxStuckRequeueAfter + time.Minute, -time.Hour} {
		if err := SetStuckRequeueAfter(bad); err == nil {
			t.Errorf("SetStuckRequeueAfter(%s) accepted, want an error", bad)
		}
	}
	if d, _ := StuckRequeueAfter(); d != 4*time.Hour {
		t.Fatalf("a rejected set changed the window to %s", d)
	}
	ResetStuckRequeueAfter()
	if d, over := StuckRequeueAfter(); d != DefaultStuckRequeueAfter || over {
		t.Fatalf("after reset = %s overridden=%v, want the default", d, over)
	}
}

// newInflightEnv builds a worker over a fresh DB with two uploads dequeued
// into processing and backdated by `idle`.
func newInflightEnv(t *testing.T, idle time.Duration) (*UploadWorker, *database.DB, *services.Upload, *services.Upload) {
	t.Helper()
	db := dbtest.OpenDB(t)
	cfg := &config.Config{
		DataDir:             t.TempDir(),
		WalletEncryptionKey: "1111111111111111111111111111111111111111111111111111111111111111",
	}
	w := NewUploadWorker(db, cfg, nil)
	if _, err := db.Exec(`INSERT INTO users (id, email) VALUES (8001, 'inflight@example.com')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	a, err := w.uploadSvc.Create(8001, nil, "a.bin", "a.bin", 10, "application/octet-stream", "private", "/tmp/a.bin", nil)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := w.uploadSvc.Create(8001, nil, "b.bin", "b.bin", 10, "application/octet-stream", "private", "/tmp/b.bin", nil)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	for range 2 {
		if _, err := w.uploadSvc.DequeueNext(); err != nil {
			t.Fatalf("dequeue: %v", err)
		}
	}
	ts := time.Now().UTC().Add(-idle).Format("2006-01-02 15:04:05")
	if _, err := db.Exec(`UPDATE uploads SET processing_at = ? WHERE status = 'processing'`, ts); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	return w, db, a, b
}

func statusOf(t *testing.T, w *UploadWorker, id int64) string {
	t.Helper()
	u, err := w.uploadSvc.GetByID(id)
	if err != nil {
		t.Fatalf("get %d: %v", id, err)
	}
	return u.Status
}

// The sweep never requeues an upload this process is working on, however
// long it has gone without progress; an orphaned one is requeued (V2-1773).
func TestSweepStuckSkipsInFlight(t *testing.T) {
	t.Cleanup(ResetStuckRequeueAfter)
	w, _, live, orphan := newInflightEnv(t, 3*time.Hour)

	w.markActive(live)
	w.sweepStuck()

	if s := statusOf(t, w, live.ID); s != "processing" {
		t.Errorf("in-flight upload status = %q, want processing", s)
	}
	if s := statusOf(t, w, orphan.ID); s != "queued" {
		t.Errorf("orphaned upload status = %q, want queued", s)
	}

	// Once the goroutine is done with it, the same row is fair game.
	w.clearActive(live.ID)
	w.sweepStuck()
	if s := statusOf(t, w, live.ID); s != "queued" {
		t.Errorf("released upload status = %q, want queued", s)
	}
}

// The operator window decides what counts as stuck.
func TestSweepStuckHonoursOverride(t *testing.T) {
	t.Cleanup(ResetStuckRequeueAfter)
	w, _, a, b := newInflightEnv(t, 90*time.Minute)

	if err := SetStuckRequeueAfter(2 * time.Hour); err != nil {
		t.Fatalf("set: %v", err)
	}
	w.sweepStuck()
	if statusOf(t, w, a.ID) != "processing" || statusOf(t, w, b.ID) != "processing" {
		t.Fatalf("90 min idle requeued under a 2h window")
	}

	ResetStuckRequeueAfter() // back to 60 min
	w.sweepStuck()
	if statusOf(t, w, a.ID) != "queued" || statusOf(t, w, b.ID) != "queued" {
		t.Fatalf("90 min idle not requeued under the 60 min default")
	}
}

// A heartbeat moves processing_at forward, so the row is no longer stuck,
// and re-arms the over-window warning for an in-flight upload.
func TestHeartbeat(t *testing.T) {
	t.Cleanup(ResetStuckRequeueAfter)
	w, _, a, b := newInflightEnv(t, 3*time.Hour)

	w.markActive(a)
	w.activeMu.Lock()
	w.active[a.ID].lastBeat = time.Now().Add(-3 * time.Hour)
	w.activeMu.Unlock()
	w.heartbeat(b, "test") // b is not in flight here: only its row is touched
	w.sweepStuck()         // keeps b (fresh heartbeat), flags a (in flight, no progress)
	if s := statusOf(t, w, b.ID); s != "processing" {
		t.Errorf("heartbeated upload status = %q, want processing", s)
	}
	w.activeMu.Lock()
	warned := w.active[a.ID].warned
	w.activeMu.Unlock()
	if !warned {
		t.Fatalf("in-flight upload past the window was not flagged")
	}

	w.heartbeat(a, "test")
	w.activeMu.Lock()
	beat, warned := w.active[a.ID].lastBeat, w.active[a.ID].warned
	w.activeMu.Unlock()
	if time.Since(beat) > time.Minute || warned {
		t.Errorf("heartbeat did not reset the in-flight entry (lastBeat %s ago, warned=%v)", time.Since(beat), warned)
	}

	w.clearActive(a.ID)
	w.sweepStuck()
	if s := statusOf(t, w, a.ID); s != "processing" {
		t.Errorf("upload with a fresh heartbeat status = %q, want processing", s)
	}
}

// refuseRepayment allows the first payment and blocks an automatic second
// one; the refusal classifies as preserve-paid (V2-1773).
func TestRefuseRepayment(t *testing.T) {
	w, _, a, _ := newInflightEnv(t, 0)

	if err := w.refuseRepayment(a); err != nil {
		t.Fatalf("first payment refused: %v", err)
	}

	wallet, err := w.walletSvc.Create("w", "0xabc", "deadbeefprivatekey")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if _, err := w.txnSvc.Record(wallet.ID, &a.ID, "upload", "100", "900", "0xhash"); err != nil {
		t.Fatalf("record txn: %v", err)
	}

	err = w.refuseRepayment(a)
	if err == nil {
		t.Fatalf("second automatic payment allowed")
	}
	if !errors.Is(err, errPaidNoRetry) {
		t.Errorf("refusal does not carry errPaidNoRetry: %v", err)
	}
	if got := classifyFailure(err, true, true); got != outcomePreservePaid {
		t.Errorf("refusal classified as %v, want outcomePreservePaid", got)
	}
}
