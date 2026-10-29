package worker

import (
	"fmt"
	"sync/atomic"
	"time"
)

// DefaultStuckRequeueAfter is how long an upload may sit in "processing" with
// no progress before the reconcile sweep puts it back in the queue. Uploads
// this process is still working on are never requeued, whatever the window
// (V2-1773).
const DefaultStuckRequeueAfter = 60 * time.Minute

// Bounds for the operator override of the stuck-upload requeue window.
const (
	MinStuckRequeueAfter = 1 * time.Minute
	MaxStuckRequeueAfter = 7 * 24 * time.Hour
)

// stuckRequeueOverride holds an operator override of the requeue window in
// nanoseconds; 0 means none. Process memory only: a restart drops it, by
// design, so a window raised to get one long upload through can't outlive
// the reason it was raised.
var stuckRequeueOverride atomic.Int64

// StuckRequeueAfter returns the requeue window in force and whether an
// operator override set it.
func StuckRequeueAfter() (window time.Duration, overridden bool) {
	if d := time.Duration(stuckRequeueOverride.Load()); d > 0 {
		return d, true
	}
	return DefaultStuckRequeueAfter, false
}

// SetStuckRequeueAfter overrides the requeue window until the process
// restarts. The window must be within [MinStuckRequeueAfter,
// MaxStuckRequeueAfter].
func SetStuckRequeueAfter(window time.Duration) error {
	if window < MinStuckRequeueAfter || window > MaxStuckRequeueAfter {
		return fmt.Errorf("requeue window must be between %s and %s", MinStuckRequeueAfter, MaxStuckRequeueAfter)
	}
	stuckRequeueOverride.Store(int64(window))
	return nil
}

// ResetStuckRequeueAfter drops the operator override, restoring the default.
func ResetStuckRequeueAfter() {
	stuckRequeueOverride.Store(0)
}
