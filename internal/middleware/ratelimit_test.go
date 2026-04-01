package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowUpToMax(t *testing.T) {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     3,
		window:  time.Minute,
	}

	for i := 0; i < 3; i++ {
		if !rl.Allow("ip1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("ip1") {
		t.Fatal("request 4 should be blocked")
	}
}

func TestRateLimiter_CheckFields(t *testing.T) {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     5,
		window:  time.Minute,
	}

	// First request
	r := rl.Check("client-a")
	if !r.Allowed {
		t.Fatal("first request should be allowed")
	}
	if r.Limit != 5 {
		t.Errorf("Limit = %d, want 5", r.Limit)
	}
	if r.Remaining != 4 {
		t.Errorf("Remaining = %d, want 4", r.Remaining)
	}
	if r.ResetAt.IsZero() {
		t.Error("ResetAt should be set")
	}

	// Consume remaining requests
	for i := 0; i < 4; i++ {
		rl.Check("client-a")
	}

	// 6th request -- over limit
	r = rl.Check("client-a")
	if r.Allowed {
		t.Fatal("6th request should be blocked")
	}
	if r.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0", r.Remaining)
	}
	if r.Limit != 5 {
		t.Errorf("Limit = %d, want 5", r.Limit)
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     1,
		window:  50 * time.Millisecond,
	}

	if !rl.Allow("ip") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("ip") {
		t.Fatal("second request should be blocked")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("ip") {
		t.Fatal("request after window expiry should be allowed")
	}
}

func TestRateLimiter_IndependentIPs(t *testing.T) {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     2,
		window:  time.Minute,
	}

	// Exhaust ip-a
	rl.Allow("ip-a")
	rl.Allow("ip-a")
	if rl.Allow("ip-a") {
		t.Fatal("ip-a should be blocked")
	}

	// ip-b should still work
	if !rl.Allow("ip-b") {
		t.Fatal("ip-b first request should be allowed")
	}
	if !rl.Allow("ip-b") {
		t.Fatal("ip-b second request should be allowed")
	}
	if rl.Allow("ip-b") {
		t.Fatal("ip-b third request should be blocked")
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     100,
		window:  time.Minute,
	}

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow("shared-ip")
		}()
	}

	wg.Wait()
	close(allowed)

	allowedCount := 0
	blockedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		} else {
			blockedCount++
		}
	}

	if allowedCount != 100 {
		t.Errorf("allowed = %d, want 100", allowedCount)
	}
	if blockedCount != 100 {
		t.Errorf("blocked = %d, want 100", blockedCount)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     1,
		window:  50 * time.Millisecond,
	}

	rl.Allow("ip-x")
	if len(rl.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(rl.entries))
	}

	time.Sleep(60 * time.Millisecond)
	rl.cleanup()

	rl.mu.Lock()
	n := len(rl.entries)
	rl.mu.Unlock()
	if n != 0 {
		t.Errorf("entries after cleanup = %d, want 0", n)
	}
}

func TestRateLimitMiddleware_AllowsWithinLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := RateLimit(3, time.Minute, nil)
	wrapped := mw(handler)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, w.Code)
		}
		if w.Header().Get("X-RateLimit-Limit") != "3" {
			t.Errorf("X-RateLimit-Limit = %q, want 3", w.Header().Get("X-RateLimit-Limit"))
		}
	}
}

func TestRateLimitMiddleware_BlocksOverLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimit(2, time.Minute, nil)
	wrapped := mw(handler)

	// Exhaust limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
	}

	// Next request should be 429
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
	if w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("X-RateLimit-Remaining = %q, want 0", w.Header().Get("X-RateLimit-Remaining"))
	}
}
