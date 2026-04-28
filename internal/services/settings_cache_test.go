package services

import (
	"testing"
	"time"
)

func TestCachedSettingsGet(t *testing.T) {
	db := setupTestDB(t)
	innerSvc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "cache@example.com", "Cache", "User")

	// Set a custom setting via the inner service
	err := innerSvc.Update(map[string]string{"custom_key": "custom_value"}, user.ID, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	cached := NewCachedSettingsService(innerSvc)

	val, err := cached.Get("custom_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "custom_value" {
		t.Errorf("expected custom_value, got %s", val)
	}
}

func TestCachedSettingsCacheHit(t *testing.T) {
	db := setupTestDB(t)
	innerSvc := NewSettingsService(db)
	cached := NewCachedSettingsService(innerSvc)

	// First call: populates cache (reads from DB)
	val1, err := cached.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	if val1 != "false" {
		t.Errorf("expected maintenance_mode=false, got %s", val1)
	}

	// Second call: should come from cache, returning the same value.
	// We verify correctness; the cache hit is implicit since the value
	// is in the internal map.
	val2, err := cached.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if val2 != val1 {
		t.Errorf("expected same value %s, got %s", val1, val2)
	}

	// Verify the value is present in the cache map
	cached.mu.RLock()
	_, inCache := cached.cache["maintenance_mode"]
	cached.mu.RUnlock()
	if !inCache {
		t.Error("expected maintenance_mode to be present in cache after Get")
	}
}

func TestCachedSettingsInvalidate(t *testing.T) {
	db := setupTestDB(t)
	innerSvc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "inval@example.com", "Inval", "User")
	cached := NewCachedSettingsService(innerSvc)

	// Populate cache
	val, err := cached.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get initial: %v", err)
	}
	if val != "false" {
		t.Fatalf("expected false, got %s", val)
	}

	// Update the underlying DB directly
	err = innerSvc.Update(map[string]string{"maintenance_mode": "true"}, user.ID, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Without invalidation, cache still returns old value
	stale, _ := cached.Get("maintenance_mode")
	if stale != "false" {
		t.Errorf("expected stale cache to return false, got %s", stale)
	}

	// Invalidate and re-fetch
	cached.Invalidate("maintenance_mode")

	fresh, err := cached.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get after invalidate: %v", err)
	}
	if fresh != "true" {
		t.Errorf("expected true after invalidation, got %s", fresh)
	}
}

func TestCachedSettingsInvalidateAll(t *testing.T) {
	db := setupTestDB(t)
	innerSvc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "invalall@example.com", "InvalAll", "User")
	cached := NewCachedSettingsService(innerSvc)

	// Populate cache with two settings
	_, err := cached.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get maintenance_mode: %v", err)
	}
	_, err = cached.Get("jwt_expiry_hours")
	if err != nil {
		t.Fatalf("Get jwt_expiry_hours: %v", err)
	}

	// Verify both are cached
	cached.mu.RLock()
	cacheLen := len(cached.cache)
	cached.mu.RUnlock()
	if cacheLen < 2 {
		t.Fatalf("expected at least 2 cached entries, got %d", cacheLen)
	}

	// Update both in DB
	err = innerSvc.Update(map[string]string{
		"maintenance_mode": "true",
		"jwt_expiry_hours": "72",
	}, user.ID, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Before InvalidateAll, cache returns stale values
	stale1, _ := cached.Get("maintenance_mode")
	stale2, _ := cached.Get("jwt_expiry_hours")
	if stale1 != "false" || stale2 != "24" {
		t.Errorf("expected stale values (false, 24), got (%s, %s)", stale1, stale2)
	}

	// InvalidateAll
	cached.InvalidateAll()

	// Verify cache is empty
	cached.mu.RLock()
	cacheLen = len(cached.cache)
	cached.mu.RUnlock()
	if cacheLen != 0 {
		t.Errorf("expected 0 cached entries after InvalidateAll, got %d", cacheLen)
	}

	// Re-fetch should get fresh values from DB
	fresh1, err := cached.Get("maintenance_mode")
	if err != nil {
		t.Fatalf("Get maintenance_mode after invalidate all: %v", err)
	}
	if fresh1 != "true" {
		t.Errorf("expected maintenance_mode=true, got %s", fresh1)
	}

	fresh2, err := cached.Get("jwt_expiry_hours")
	if err != nil {
		t.Fatalf("Get jwt_expiry_hours after invalidate all: %v", err)
	}
	if fresh2 != "72" {
		t.Errorf("expected jwt_expiry_hours=72, got %s", fresh2)
	}

	// Suppress unused variable warning for time import used in TTL-related tests
	_ = time.Second
}

func TestGetIntWithBounds(t *testing.T) {
	db := setupTestDB(t)
	innerSvc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "bounds@example.com", "Bounds", "User")
	cached := NewCachedSettingsService(innerSvc)

	// Migration 002 seeds antd_quote_timeout_secs=300 — verify the in-range path.
	if got := cached.GetIntWithBounds("antd_quote_timeout_secs", 999, 1, 3600); got != 300 {
		t.Errorf("seeded value: got %d, want 300", got)
	}

	// Missing key → fallback.
	if got := cached.GetIntWithBounds("nonexistent_key", 42, 1, 100); got != 42 {
		t.Errorf("missing key: got %d, want 42", got)
	}

	// Set values that exercise the rejection branches. Use untyped keys so
	// SettingsService.Update doesn't refuse them at write time — we want to
	// observe what GetIntWithBounds does when bad data is already in the DB
	// (predates the validator, or was set out-of-band).
	bad := map[string]string{
		"untyped_garbage": "abc",
		"untyped_low":     "0",
		"untyped_high":    "1000",
		"untyped_empty":   "",
	}
	if err := innerSvc.Update(bad, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("seed bad values: %v", err)
	}

	cases := []struct {
		key      string
		fallback int
		min, max int
		want     int
		desc     string
	}{
		{"untyped_garbage", 7, 1, 100, 7, "non-numeric"},
		{"untyped_low", 7, 1, 100, 7, "below min"},
		{"untyped_high", 7, 1, 100, 7, "above max"},
		{"untyped_empty", 7, 1, 100, 7, "empty string"},
	}
	for _, c := range cases {
		// Fresh cache per case so the previous Get doesn't mask issues.
		cached.InvalidateAll()
		if got := cached.GetIntWithBounds(c.key, c.fallback, c.min, c.max); got != c.want {
			t.Errorf("%s: got %d, want %d", c.desc, got, c.want)
		}
	}

	// In-range value persisted via Update should be returned verbatim.
	if err := innerSvc.Update(map[string]string{"antd_quote_timeout_secs": "120"}, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("update valid: %v", err)
	}
	cached.InvalidateAll()
	if got := cached.GetIntWithBounds("antd_quote_timeout_secs", 300, 1, 3600); got != 120 {
		t.Errorf("updated value: got %d, want 120", got)
	}
}
