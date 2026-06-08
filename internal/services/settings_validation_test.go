package services

import (
	"errors"
	"testing"
)

func TestIntInRange(t *testing.T) {
	v := intInRange(1, 100)

	cases := []struct {
		in      string
		wantErr bool
		desc    string
	}{
		{"1", false, "min boundary"},
		{"50", false, "middle"},
		{"100", false, "max boundary"},
		{"0", true, "below min"},
		{"101", true, "above max"},
		{"-5", true, "negative"},
		{"abc", true, "not an integer"},
		{"", true, "empty string"},
		{"1.5", true, "decimal"},
	}
	for _, c := range cases {
		err := v(c.in)
		if c.wantErr && err == nil {
			t.Errorf("%s (%q): expected error, got nil", c.desc, c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s (%q): expected nil, got %v", c.desc, c.in, err)
		}
	}
}

func TestOptionalIntInRange(t *testing.T) {
	v := optionalIntInRange(30, 3600)
	cases := []struct {
		in      string
		wantErr bool
		desc    string
	}{
		{"", false, "empty means use default"},
		{"30", false, "min boundary"},
		{"300", false, "typical"},
		{"3600", false, "max boundary"},
		{"29", true, "below min"},
		{"3601", true, "above max"},
		{"abc", true, "non-numeric"},
	}
	for _, c := range cases {
		err := v(c.in)
		if c.wantErr && err == nil {
			t.Errorf("%s (%q): expected error, got nil", c.desc, c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s (%q): expected nil, got %v", c.desc, c.in, err)
		}
	}
}

func TestSettingsUpdatePaymentConfirmationTimeout(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "pct@example.com", "PCT", "User")

	// A valid value is accepted via the PATCH path.
	if err := svc.Update(map[string]string{"payment_confirmation_timeout_seconds": "600"}, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("valid timeout rejected: %v", err)
	}
	// Clearing it (empty) is accepted → worker falls back to the signer default.
	if err := svc.Update(map[string]string{"payment_confirmation_timeout_seconds": ""}, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("empty timeout rejected: %v", err)
	}
	// An out-of-range value is rejected.
	if err := svc.Update(map[string]string{"payment_confirmation_timeout_seconds": "5"}, user.ID, "127.0.0.1", "TestAgent"); err == nil {
		t.Fatal("expected below-min timeout to be rejected")
	}
}

func TestSettingsUpdateValidatesTypedKeys(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "validator@example.com", "Validator", "User")

	// Valid values for both registered timeout keys.
	if err := svc.Update(map[string]string{
		"antd_quote_timeout_secs":        "150",
		"antd_health_probe_timeout_secs": "30",
	}, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("valid Update returned error: %v", err)
	}
	if v, _ := svc.Get("antd_quote_timeout_secs"); v != "150" {
		t.Errorf("antd_quote_timeout_secs: got %q, want 150", v)
	}
	if v, _ := svc.Get("antd_health_probe_timeout_secs"); v != "30" {
		t.Errorf("antd_health_probe_timeout_secs: got %q, want 30", v)
	}
}

func TestSettingsUpdateRejectsInvalidTypedKeys(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "rejecter@example.com", "Rejecter", "User")

	cases := []struct {
		key, value, desc string
	}{
		{"antd_quote_timeout_secs", "-1", "negative"},
		{"antd_quote_timeout_secs", "0", "below min"},
		{"antd_quote_timeout_secs", "3601", "above max"},
		{"antd_quote_timeout_secs", "abc", "non-numeric"},
		{"antd_health_probe_timeout_secs", "0", "health below min"},
		{"antd_health_probe_timeout_secs", "121", "health above max"},
	}
	for _, c := range cases {
		err := svc.Update(map[string]string{c.key: c.value}, user.ID, "127.0.0.1", "TestAgent")
		if err == nil {
			t.Errorf("%s (%s=%q): expected error, got nil", c.desc, c.key, c.value)
			continue
		}
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%s: expected *ValidationError, got %T: %v", c.desc, err, err)
		}
		if verr != nil && verr.Key != c.key {
			t.Errorf("%s: ValidationError.Key = %q, want %q", c.desc, verr.Key, c.key)
		}
	}
}

func TestSettingsUpdateRejectsBatchAtomically(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "atomic@example.com", "Atomic", "User")

	// Capture the seeded default (300) before the bad batch.
	before, _ := svc.Get("antd_quote_timeout_secs")

	// One valid + one invalid in the same PATCH should reject the whole batch
	// — the valid key must NOT be persisted.
	err := svc.Update(map[string]string{
		"antd_quote_timeout_secs":        "200", // valid in isolation
		"antd_health_probe_timeout_secs": "999", // invalid (>120)
	}, user.ID, "127.0.0.1", "TestAgent")
	if err == nil {
		t.Fatal("expected ValidationError, got nil")
	}

	after, _ := svc.Get("antd_quote_timeout_secs")
	if after != before {
		t.Errorf("partial write detected: before=%q, after=%q (should be unchanged)", before, after)
	}
}

func TestSettingsUpdateAcceptsUntypedKeys(t *testing.T) {
	// Untyped keys (no entry in typedValidators) bypass validation —
	// preserves backward compatibility with all the pre-existing settings.
	db := setupTestDB(t)
	svc := NewSettingsService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "untyped@example.com", "Untyped", "User")

	if err := svc.Update(map[string]string{
		"some_random_new_key": "any value, even garbage like !!@#",
		"environment_name":    "staging",
	}, user.ID, "127.0.0.1", "TestAgent"); err != nil {
		t.Fatalf("untyped Update returned error: %v", err)
	}
}
