package services

import (
	"sync"
	"time"
)

// CachedSettingsService wraps SettingsService with an in-memory TTL cache.
type CachedSettingsService struct {
	inner *SettingsService
	mu    sync.RWMutex
	cache map[string]cachedValue
	ttl   time.Duration
}

type cachedValue struct {
	value     string
	expiresAt time.Time
}

// NewCachedSettingsService creates a settings service with a 30-second cache.
func NewCachedSettingsService(inner *SettingsService) *CachedSettingsService {
	return &CachedSettingsService{
		inner: inner,
		cache: make(map[string]cachedValue),
		ttl:   30 * time.Second,
	}
}

// Get returns a cached setting, fetching from DB if expired or missing.
func (c *CachedSettingsService) Get(key string) (string, error) {
	c.mu.RLock()
	if cv, ok := c.cache[key]; ok && time.Now().Before(cv.expiresAt) {
		c.mu.RUnlock()
		return cv.value, nil
	}
	c.mu.RUnlock()

	// Cache miss — fetch from DB
	val, err := c.inner.Get(key)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.cache[key] = cachedValue{value: val, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return val, nil
}

// Invalidate removes a key from the cache.
func (c *CachedSettingsService) Invalidate(key string) {
	c.mu.Lock()
	delete(c.cache, key)
	c.mu.Unlock()
}

// InvalidateAll clears the entire cache.
func (c *CachedSettingsService) InvalidateAll() {
	c.mu.Lock()
	c.cache = make(map[string]cachedValue)
	c.mu.Unlock()
}
