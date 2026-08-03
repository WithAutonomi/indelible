package handlers

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fixedLimit builds a limitFn for tests where the cap never changes.
func fixedLimit(n int) func() int { return func() int { return n } }

func TestDownloadGate_AdmitsUpToLimit(t *testing.T) {
	g := newDownloadGate(fixedLimit(3))
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if !g.acquire(ctx, 0) {
			t.Fatalf("acquire %d rejected below the limit", i+1)
		}
	}
	if g.acquire(ctx, 0) {
		t.Fatal("acquire admitted past the limit with zero wait")
	}
}

func TestDownloadGate_ReleaseAdmitsWaiter(t *testing.T) {
	g := newDownloadGate(fixedLimit(1))
	ctx := context.Background()

	if !g.acquire(ctx, 0) {
		t.Fatal("first acquire rejected")
	}

	admitted := make(chan bool, 1)
	go func() { admitted <- g.acquire(ctx, 5*time.Second) }()

	// The waiter must be blocked, not admitted, while the slot is held.
	select {
	case got := <-admitted:
		t.Fatalf("waiter returned %v while the slot was still held", got)
	case <-time.After(50 * time.Millisecond):
	}

	g.release()
	select {
	case got := <-admitted:
		if !got {
			t.Fatal("waiter rejected after a slot was released")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never woke after release")
	}
}

func TestDownloadGate_WaitTimesOut(t *testing.T) {
	g := newDownloadGate(fixedLimit(1))
	ctx := context.Background()

	if !g.acquire(ctx, 0) {
		t.Fatal("first acquire rejected")
	}
	start := time.Now()
	if g.acquire(ctx, 50*time.Millisecond) {
		t.Fatal("acquire admitted past the limit")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("acquire gave up after %v, before the %v wait elapsed", elapsed, 50*time.Millisecond)
	}
}

func TestDownloadGate_ContextCancelUnblocks(t *testing.T) {
	g := newDownloadGate(fixedLimit(1))
	if !g.acquire(context.Background(), 0) {
		t.Fatal("first acquire rejected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	admitted := make(chan bool, 1)
	go func() { admitted <- g.acquire(ctx, time.Minute) }()

	cancel()
	select {
	case got := <-admitted:
		if got {
			t.Fatal("acquire admitted after context cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("acquire did not return promptly on context cancellation")
	}
}

// A lowered limit must drain excess in-flight downloads before admitting new
// ones — and never below the new cap once drained.
func TestDownloadGate_LoweredLimitDrains(t *testing.T) {
	var limit atomic.Int64
	limit.Store(2)
	g := newDownloadGate(func() int { return int(limit.Load()) })
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if !g.acquire(ctx, 0) {
			t.Fatalf("setup acquire %d rejected", i+1)
		}
	}

	// Operator lowers the cap to 1: 2 in flight, no admission.
	limit.Store(1)
	if g.acquire(ctx, 0) {
		t.Fatal("admitted while in-flight count exceeded the lowered limit")
	}
	g.release()
	// 1 in flight == new cap of 1: still full.
	if g.acquire(ctx, 0) {
		t.Fatal("admitted while in-flight count equalled the lowered limit")
	}
	g.release()
	// Drained below the cap: admission resumes.
	if !g.acquire(ctx, 0) {
		t.Fatal("rejected after draining below the lowered limit")
	}
}

// Regression for the stale-waiter finding on PR #145: a request that queued
// while the cap was 2 must not admit against that captured value once the
// operator lowers the cap to 1 — every admission attempt, including a parked
// waiter waking on release, re-reads the current limit.
func TestDownloadGate_QueuedWaiterHonorsLoweredLimit(t *testing.T) {
	var limit atomic.Int64
	limit.Store(2)
	g := newDownloadGate(func() int { return int(limit.Load()) })
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if !g.acquire(ctx, 0) {
			t.Fatalf("setup acquire %d rejected", i+1)
		}
	}

	// W queues while the cap is 2 and both slots are held.
	admitted := make(chan bool, 1)
	go func() { admitted <- g.acquire(ctx, 5*time.Second) }()
	select {
	case got := <-admitted:
		t.Fatalf("waiter returned %v while both slots were held", got)
	case <-time.After(50 * time.Millisecond):
	}

	// Operator lowers the cap to 1, then one active download finishes,
	// waking W with inFlight == 1 == the new cap. Under the old bug W
	// compared against its captured limit of 2, saw 1 < 2, and re-admitted —
	// defeating the drain the operator asked for.
	limit.Store(1)
	g.release()
	select {
	case got := <-admitted:
		t.Fatalf("waiter returned %v against the lowered limit", got)
	case <-time.After(100 * time.Millisecond):
	}

	// Draining below the new cap admits W.
	g.release()
	select {
	case got := <-admitted:
		if !got {
			t.Fatal("waiter rejected after draining below the lowered limit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never admitted after draining below the lowered limit")
	}
}
