package services

import "testing"

func TestRedactWebhookURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"slack secret in path", "https://hooks.slack.com/services/T000/B000/XXXXSECRET", "https://hooks.slack.com"},
		{"query string dropped", "https://example.com/hook?token=secret", "https://example.com"},
		{"host only", "https://example.com", "https://example.com"},
		{"unparseable", "://nonsense", "redacted"},
		{"empty", "", "redacted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactWebhookURL(tc.in)
			if got != tc.want {
				t.Errorf("RedactWebhookURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// The credential-bearing path/query must never survive.
			if got != "redacted" && (len(got) > 0 && got[len(got)-1] == '/') {
				t.Errorf("RedactWebhookURL(%q) = %q leaked a path separator", tc.in, got)
			}
		})
	}
}
