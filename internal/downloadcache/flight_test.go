package downloadcache

import (
	"sync"
	"testing"
	"time"
)

func TestFlightSingleLeader(t *testing.T) {
	f := NewFlight()

	leader, _ := f.Begin(testKey)
	if !leader {
		t.Fatal("first Begin must lead")
	}
	follower, done := f.Begin(testKey)
	if follower {
		t.Fatal("second Begin must follow, not lead")
	}

	select {
	case <-done:
		t.Fatal("follower woke before Finish")
	case <-time.After(20 * time.Millisecond):
	}

	f.Finish(testKey)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("follower never woke after Finish")
	}

	// The key is free again: the next Begin leads a fresh fill.
	if leader, _ := f.Begin(testKey); !leader {
		t.Fatal("Begin after Finish must lead")
	}
	f.Finish(testKey)
}

func TestFlightIndependentKeys(t *testing.T) {
	f := NewFlight()
	otherKey := "fedcba" + testKey[6:]

	if leader, _ := f.Begin(testKey); !leader {
		t.Fatal("first key must lead")
	}
	if leader, _ := f.Begin(otherKey); !leader {
		t.Fatal("distinct key must lead its own fill")
	}
	f.Finish(testKey)
	f.Finish(otherKey)
}

func TestFlightConcurrentBegins(t *testing.T) {
	f := NewFlight()
	var leaders sync.Map
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if leader, _ := f.Begin(testKey); leader {
				leaders.Store(i, true)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	n := 0
	leaders.Range(func(_, _ any) bool { n++; return true })
	if n != 1 {
		t.Fatalf("%d leaders for one key, want exactly 1", n)
	}
	f.Finish(testKey)
}

func TestCounterBumpAndReset(t *testing.T) {
	c := NewCounter(2)

	if got := c.Bump("a"); got != 1 {
		t.Fatalf("first bump = %d, want 1", got)
	}
	if got := c.Bump("a"); got != 2 {
		t.Fatalf("second bump = %d, want 2", got)
	}
	c.Bump("b") // at cap: existing keys keep counting

	// A third distinct key exceeds cap: wholesale reset, then counts restart.
	if got := c.Bump("c"); got != 1 {
		t.Fatalf("post-reset bump = %d, want 1", got)
	}
	if got := c.Bump("a"); got != 1 {
		t.Fatalf("count survived the reset: %d, want 1", got)
	}
}
