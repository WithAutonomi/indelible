package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

// RateLimitResult contains the result of a rate limit check.
type RateLimitResult struct {
	Allowed   bool
	Limit     int
	Remaining int
	ResetAt   time.Time
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
	return rl.Check(key).Allowed
}

// Check returns detailed rate limit info for the given key.
func (rl *RateLimiter) Check(key string) RateLimitResult {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, ok := rl.entries[key]
	if !ok || now.After(entry.resetAt) {
		rl.entries[key] = &rateLimitEntry{count: 1, resetAt: now.Add(rl.window)}
		return RateLimitResult{
			Allowed:   true,
			Limit:     rl.max,
			Remaining: rl.max - 1,
			ResetAt:   now.Add(rl.window),
		}
	}
	entry.count++
	remaining := rl.max - entry.count
	if remaining < 0 {
		remaining = 0
	}
	return RateLimitResult{
		Allowed:   entry.count <= rl.max,
		Limit:     rl.max,
		Remaining: remaining,
		ResetAt:   entry.resetAt,
	}
}

// RateLimitByUser returns middleware that limits requests per authenticated user ID.
func RateLimitByUser(max int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(max, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			key := fmt.Sprintf("user:%d", userID)
			result := limiter.Check(key)

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))

			if !result.Allowed {
				retryAfter := int(time.Until(result.ResetAt).Seconds()) + 1
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"upload rate limit exceeded, please try again later","code":"rate_limit_exceeded"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit returns middleware that limits requests per IP.
// trustedProxies controls whether X-Forwarded-For is honoured.
func RateLimit(max int, window time.Duration, trustedProxies []string) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(max, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, trustedProxies)
			result := limiter.Check(ip)

			// Always set rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))

			if !result.Allowed {
				retryAfter := int(time.Until(result.ResetAt).Seconds()) + 1
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"too many requests, please try again later","code":"rate_limit_exceeded"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
