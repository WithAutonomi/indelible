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
// The limit is passed to acquire on every call (read from settings per
// request), so the cap is live-tunable: lowering it below the current
// in-flight count admits no new downloads until the excess drains, and never
// interrupts a download already running.
type downloadGate struct {
	mu       sync.Mutex
	inFlight int
	// notify is closed and replaced on every release, waking all waiters to
	// re-check for a free slot (generation-channel broadcast).
	notify chan struct{}
}

func newDownloadGate() *downloadGate {
	return &downloadGate{notify: make(chan struct{})}
}

// acquire claims a download slot, waiting up to maxWait for one to free. It
// returns false if the wait elapses or ctx is done first. Every true return
// must be paired with exactly one release.
func (g *downloadGate) acquire(ctx context.Context, limit int, maxWait time.Duration) bool {
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	for {
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
			// A slot freed; loop and race the other waiters for it.
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
