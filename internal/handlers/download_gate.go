package handlers

import (
	"context"
	"sync"
	"time"
)

// downloadGate bounds how many downloads may hold an antd fetch (and its temp
// file) at once (V2-809). Nothing else in the chain limits file-level download
// concurrency: antd accepts unlimited concurrent /v1/files/get requests, and
// every in-flight download splits one client-wide adaptive chunk-fetch budget
// while materialising the full file under DataDir/uploads/tmp — the same
// volume the disk-critical pause watches, so unbounded downloads could pause
// uploads. Admission control therefore lives here, the only layer that knows
// the disk and timeout budgets.
//
// The effective limit is re-read via limitFn on every admission attempt —
// fresh arrivals and parked waiters re-checking after a release alike — so the
// cap is live-tunable and a request queued before an operator lowered it never
// admits against the obsolete value: lowering the cap below the current
// in-flight count admits no new downloads until the excess drains, and never
// interrupts a download already running.
type downloadGate struct {
	// limitFn returns the current concurrency cap. Called outside the mutex on
	// every admission attempt (it may touch the settings cache/DB, and a slow
	// read must never block release or other admissions).
	limitFn func() int

	mu       sync.Mutex
	inFlight int
	// notify is closed and replaced on every release, waking all waiters to
	// re-check for a free slot (generation-channel broadcast).
	notify chan struct{}
}

func newDownloadGate(limitFn func() int) *downloadGate {
	return &downloadGate{limitFn: limitFn, notify: make(chan struct{})}
}

// acquire claims a download slot, waiting up to maxWait for one to free. It
// returns false if the wait elapses or ctx is done first. Every true return
// must be paired with exactly one release.
func (g *downloadGate) acquire(ctx context.Context, maxWait time.Duration) bool {
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	for {
		limit := g.limitFn()
		g.mu.Lock()
		if g.inFlight < limit {
			g.inFlight++
			g.mu.Unlock()
			return true
		}
		notify := g.notify
		g.mu.Unlock()

		select {
		case <-notify:
			// A slot freed; loop to re-read the limit and race the other
			// waiters for it.
		case <-timer.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// release frees a slot and wakes all waiters.
func (g *downloadGate) release() {
	g.mu.Lock()
	g.inFlight--
	close(g.notify)
	g.notify = make(chan struct{})
	g.mu.Unlock()
}
