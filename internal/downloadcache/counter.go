package downloadcache

import "sync"

// Counter tracks per-key access counts for min-uses admission (V2-821, the
// nginx proxy_cache_min_uses pattern): an object is only promoted once it has
// been requested N times, keeping one-hit wonders from evicting entries that
// would have hit. Counts are per-instance and in-memory, like every other
// piece of cache recency state (share-nothing readers count independently).
//
// Memory is bounded by wholesale reset: when the map exceeds cap entries it
// is cleared. That forgets in-progress counts — an object mid-way to
// admission starts over — which only delays caching, never corrupts it; the
// trade buys a hard memory ceiling with no eviction bookkeeping.
type Counter struct {
	mu     sync.Mutex
	counts map[string]int
	cap    int
}

func NewCounter(cap int) *Counter {
	return &Counter{counts: make(map[string]int), cap: cap}
}

// Bump increments key's count and returns the new value.
func (c *Counter) Bump(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.counts) >= c.cap {
		if _, tracked := c.counts[key]; !tracked {
			c.counts = make(map[string]int)
		}
	}
	c.counts[key]++
	return c.counts[key]
}
