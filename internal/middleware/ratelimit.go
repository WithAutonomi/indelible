package middleware

import (
	"net/http"
	"sync"
	"time"
)

type rateLimitEntry struct {
	count    int
	resetAt  time.Time
}

// RateLimiter is a simple in-memory rate limiter keyed by IP address.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
	max     int
	window  time.Duration
}

// NewRateLimiter creates a rate limiter allowing max requests per window.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     max,
		window:  window,
	}
	// Background cleanup every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for k, e := range rl.entries {
		if now.After(e.resetAt) {
			delete(rl.entries, k)
		}
	}
}

// Allow checks if the given key is within the rate limit.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, ok := rl.entries[key]
	if !ok || now.After(entry.resetAt) {
		rl.entries[key] = &rateLimitEntry{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	entry.count++
	return entry.count <= rl.max
}

// RateLimit returns middleware that limits requests per IP.
func RateLimit(max int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(max, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				ip = fwd
			}
			if !limiter.Allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"too many requests, please try again later"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
