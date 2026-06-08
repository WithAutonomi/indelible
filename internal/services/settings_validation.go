package services

import (
	"fmt"
	"strconv"
)

// ValidationError is returned by SettingsService.Update when a typed-key value
// fails validation. Handlers should surface this as 400 Bad Request rather than 500.
type ValidationError struct {
	Key    string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid value for %q: %s", e.Key, e.Reason)
}

// typedValidators maps known-typed setting keys to their validators.
// SettingsService.Update consults this map and rejects PATCH calls with bad
// values before touching the database.
//
// Read sites should *also* clamp via CachedSettingsService.GetIntWithBounds —
// values that predate a validator (or were set via direct DB write) won't be
// caught at write time, and the read-side clamp is the safety net.
var typedValidators = map[string]func(string) error{
	"antd_quote_timeout_secs":              intInRange(1, 3600),
	"antd_health_probe_timeout_secs":       intInRange(1, 120),
	"notifier_method":                      oneOf("auto", "smtp", "webhook", "noop"),
	"registration_enabled":                 oneOf("true", "false"),
	"payment_confirmation_timeout_seconds": optionalIntInRange(30, 3600),
}

// oneOf builds a validator that requires the value to be in the allowed set.
func oneOf(allowed ...string) func(string) error {
	return func(s string) error {
		for _, a := range allowed {
			if s == a {
				return nil
			}
		}
		return fmt.Errorf("must be one of: %v", allowed)
	}
}

// optionalIntInRange is intInRange but treats an empty string as "unset" (valid),
// so a setting can be cleared in the UI to fall back to its built-in default.
func optionalIntInRange(min, max int) func(string) error {
	inRange := intInRange(min, max)
	return func(s string) error {
		if s == "" {
			return nil
		}
		return inRange(s)
	}
}

// intInRange builds a validator that requires the value to parse as an int in [min,max].
func intInRange(min, max int) func(string) error {
	return func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be an integer: %w", err)
		}
		if n < min || n > max {
			return fmt.Errorf("must be between %d and %d", min, max)
		}
		return nil
	}
}
