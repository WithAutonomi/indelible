package downloadcache

import "sync"

// Flight coalesces concurrent fills of the same key (V2-821): when several
// requests miss on one object at once, only the leader should burn a download
// gate slot and an antd fetch; the rest wait for the fill and then serve the
// promoted bytes from disk. Immutability removes the refresh case, so this is
// only ever needed on fill.
//
// Callers own the timeout policy: a waiter that gives up must proceed via its
// own fetch WITHOUT promoting (the nginx 1.7.8 lesson — a timed-out waiter
// caching its response reintroduces the stampede the flight exists to stop).
type Flight struct {
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

func NewFlight() *Flight {
	return &Flight{inflight: make(map[string]chan struct{})}
}

// Begin joins or starts the fill for key. The first caller becomes the leader
// (leader=true) and MUST call Finish(key) when its fill attempt is over —
// success or failure — or followers wait out their full timeout. Followers
// get leader=false and a channel that closes at Finish.
func (f *Flight) Begin(key string) (leader bool, done <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.inflight[key]; ok {
		return false, ch
	}
	ch := make(chan struct{})
	f.inflight[key] = ch
	return true, ch
}

// Finish ends key's fill, waking all followers. Idempotent per Begin: only
// the registered channel is closed and removed.
func (f *Flight) Finish(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.inflight[key]; ok {
		close(ch)
		delete(f.inflight, key)
	}
}
