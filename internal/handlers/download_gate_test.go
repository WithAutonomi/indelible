package handlers

import (
	"context"
	"testing"
	"time"
)

func TestDownloadGate_AdmitsUpToLimit(t *testing.T) {
	g := newDownloadGate()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if !g.acquire(ctx, 3, 0) {
			t.Fatalf("acquire %d rejected below the limit", i+1)
		}
	}
	if g.acquire(ctx, 3, 0) {
		t.Fatal("acquire admitted past the limit with zero wait")
	}
}

func TestDownloadGate_ReleaseAdmitsWaiter(t *testing.T) {
	g := newDownloadGate()
	ctx := context.Background()

	if !g.acquire(ctx, 1, 0) {
		t.Fatal("first acquire rejected")
	}

	admitted := make(chan bool, 1)
	go func() { admitted <- g.acquire(ctx, 1, 5*time.Second) }()

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
	g := newDownloadGate()
	ctx := context.Background()

	if !g.acquire(ctx, 1, 0) {
		t.Fatal("first acquire rejected")
	}
	start := time.Now()
	if g.acquire(ctx, 1, 50*time.Millisecond) {
		t.Fatal("acquire admitted past the limit")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("acquire gave up after %v, before the %v wait elapsed", elapsed, 50*time.Millisecond)
	}
}

func TestDownloadGate_ContextCancelUnblocks(t *testing.T) {
	g := newDownloadGate()
	if !g.acquire(context.Background(), 1, 0) {
		t.Fatal("first acquire rejected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	admitted := make(chan bool, 1)
	go func() { admitted <- g.acquire(ctx, 1, time.Minute) }()

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
	g := newDownloadGate()
	ctx := context.Background()

	if !g.acquire(ctx, 2, 0) || !g.acquire(ctx, 2, 0) {
		t.Fatal("setup acquires rejected")
	}

	// Operator lowers the cap to 1: 2 in flight, no admission.
	if g.acquire(ctx, 1, 0) {
		t.Fatal("admitted while in-flight count exceeded the lowered limit")
	}
	g.release()
	// 1 in flight == new cap of 1: still full.
	if g.acquire(ctx, 1, 0) {
		t.Fatal("admitted while in-flight count equalled the lowered limit")
	}
	g.release()
	// Drained below the cap: admission resumes.
	if !g.acquire(ctx, 1, 0) {
		t.Fatal("rejected after draining below the lowered limit")
	}
}
