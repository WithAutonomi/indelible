package worker

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/WithAutonomi/indelible/internal/services"
)

// activeUpload is an upload this process is working on, from dequeue until
// processOne returns.
type activeUpload struct {
	uuid     string
	lastBeat time.Time // dequeue, or the latest progress heartbeat
	warned   bool      // the over-window warning has been logged since lastBeat
}

// markActive records that this process is now working on an upload. The
// stuck-upload sweep never requeues it (V2-1773).
func (w *UploadWorker) markActive(u *services.Upload) {
	w.activeMu.Lock()
	defer w.activeMu.Unlock()
	if w.active == nil {
		w.active = make(map[int64]*activeUpload)
	}
	w.active[u.ID] = &activeUpload{uuid: u.UUID, lastBeat: time.Now()}
}

// clearActive records that this process has stopped working on an upload.
func (w *UploadWorker) clearActive(id int64) {
	w.activeMu.Lock()
	defer w.activeMu.Unlock()
	delete(w.active, id)
}

// activeIDs returns the uploads this process is working on.
func (w *UploadWorker) activeIDs() []int64 {
	w.activeMu.Lock()
	defer w.activeMu.Unlock()
	ids := make([]int64, 0, len(w.active))
	for id := range w.active {
		ids = append(ids, id)
	}
	return ids
}

// heartbeat records progress on an upload at a phase boundary: processing_at
// moves forward, so the sweep's window measures time without progress
// (V2-1773). Best-effort: a failed write only makes the row look older.
func (w *UploadWorker) heartbeat(u *services.Upload, phase string) {
	if err := w.uploadSvc.TouchProcessing(u.ID); err != nil {
		slog.Warn("upload heartbeat failed", "uuid", u.UUID, "phase", phase, "error", err)
	}
	w.activeMu.Lock()
	defer w.activeMu.Unlock()
	if a, ok := w.active[u.ID]; ok {
		a.lastBeat = time.Now()
		a.warned = false
	}
}

// sweepStuck puts uploads that have made no progress for the requeue window
// back in the queue, skipping the ones this process is still working on.
// Requeuing a live upload let a second goroutine prepare and pay for it again
// (V2-1773); instead, an in-flight upload past the window is logged once.
func (w *UploadWorker) sweepStuck() {
	window, overridden := StuckRequeueAfter()
	requeued, err := w.uploadSvc.RequeueStuckExcept(window, w.activeIDs())
	if err != nil {
		slog.Error("reconciliation requeue failed", "error", err)
	} else if requeued > 0 {
		slog.Info("reconciliation: requeued stuck uploads", "count", requeued, "window", window, "window_overridden", overridden)
	}

	w.activeMu.Lock()
	defer w.activeMu.Unlock()
	for id, a := range w.active {
		if since := time.Since(a.lastBeat); since > window && !a.warned {
			a.warned = true
			slog.Warn("upload in flight with no progress past the requeue window — not requeued",
				"upload_id", id, "uuid", a.uuid, "no_progress_for", since.Round(time.Second), "window", window)
		}
	}
}

// refuseRepayment returns an error when a payment has already been recorded
// for the upload since it was created or last retried by an operator: an
// automatic re-run (sweep or startup requeue, an in-attempt retry, gas
// backoff) of an upload that has paid, which must not pay again (V2-1773).
// The error carries errPaidNoRetry, so the upload fails as paid_unfinalized
// with its source kept. An operator Retry raises the upload's payment floor
// past its earlier payments, which authorizes one new payment. A payment the
// ledger failed to record is invisible here; paidNoRetryIfUnrecorded covers
// the rest of that attempt.
func (w *UploadWorker) refuseRepayment(u *services.Upload) error {
	paid, err := w.txnSvc.HasPaymentSinceQueued(u.ID)
	if err != nil {
		// Can't show that paying is safe, so don't pay.
		return fmt.Errorf("could not check for an earlier payment, not paying: %w", err)
	}
	if paid {
		slog.Warn("payment refused: one is already recorded since this upload was queued",
			"uuid", u.UUID)
		return fmt.Errorf("a payment is already recorded for this upload since it was queued; not paying again automatically (retry the upload to authorize a new payment): %w", errPaidNoRetry)
	}
	return nil
}
