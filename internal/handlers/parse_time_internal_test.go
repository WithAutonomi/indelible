package handlers

import (
	"testing"
	"time"
)

func TestParseTimeParam(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantNil bool
		wantUTC string // expected time formatted as RFC3339 in UTC
	}{
		{"empty", "", true, ""},
		{"garbage", "not-a-date", true, ""},
		{"date only", "2026-06-02", false, "2026-06-02T00:00:00Z"},
		{"rfc3339 utc", "2026-06-02T10:30:00Z", false, "2026-06-02T10:30:00Z"},
		{"rfc3339 offset normalised to utc", "2026-06-02T10:30:00+02:00", false, "2026-06-02T08:30:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseTimeParam(c.in)
			if c.wantNil {
				if got != nil {
					t.Fatalf("parseTimeParam(%q) = %v, want nil", c.in, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseTimeParam(%q) = nil, want %s", c.in, c.wantUTC)
			}
			if g := got.UTC().Format(time.RFC3339); g != c.wantUTC {
				t.Fatalf("parseTimeParam(%q) = %s, want %s", c.in, g, c.wantUTC)
			}
		})
	}
}
