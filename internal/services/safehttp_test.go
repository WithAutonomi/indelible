package services

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},       // loopback v4
		{"::1", true},             // loopback v6
		{"10.0.0.5", true},        // RFC1918
		{"172.16.0.1", true},      // RFC1918
		{"192.168.1.1", true},     // RFC1918
		{"169.254.169.254", true}, // link-local (cloud metadata)
		{"fe80::1", true},         // link-local v6
		{"fc00::1", true},         // ULA (IsPrivate)
		{"0.0.0.0", true},         // unspecified
		{"224.0.0.1", true},       // multicast
		{"8.8.8.8", false},        // public
		{"1.1.1.1", false},        // public
		{"93.184.216.34", false},  // public (example.com)
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := isBlockedIP(ip); got != c.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
	if !isBlockedIP(nil) {
		t.Error("isBlockedIP(nil) should be blocked")
	}
}

func TestSSRFControl(t *testing.T) {
	// A non-loopback private address is blocked (loopback is relaxed under test).
	if err := ssrfControl("tcp", "10.0.0.1:443", nil); err == nil {
		t.Error("ssrfControl should block a private (10.0.0.1) destination")
	}
	if err := ssrfControl("tcp", "169.254.169.254:80", nil); err == nil {
		t.Error("ssrfControl should block the cloud-metadata address")
	}
	// A public address is allowed.
	if err := ssrfControl("tcp", "8.8.8.8:443", nil); err != nil {
		t.Errorf("ssrfControl should allow a public destination, got %v", err)
	}
}

func TestValidateOutboundURL(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://hooks.example.com/abc", false},
		{"http://example.com", false},
		{"ftp://example.com", true},
		{"file:///etc/passwd", true},
		{"https://", true},  // no host
		{"not a url", true}, // no scheme/host
		{"", true},          // empty
		{"javascript:alert(1)", true},
	}
	for _, c := range cases {
		err := validateOutboundURL(c.url)
		if c.wantErr && err == nil {
			t.Errorf("validateOutboundURL(%q): expected error, got nil", c.url)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateOutboundURL(%q): expected nil, got %v", c.url, err)
		}
	}
}

func TestEscapeSlackText(t *testing.T) {
	cases := map[string]string{
		"plain":                "plain",
		"a & b":                "a &amp; b",
		"<script>":             "&lt;script&gt;",
		"<https://evil|click>": "&lt;https://evil|click&gt;", // mrkdwn link injection neutralised
		"*not bold* but text":  "*not bold* but text",        // * left as-is (cosmetic only)
	}
	for in, want := range cases {
		if got := escapeSlackText(in); got != want {
			t.Errorf("escapeSlackText(%q) = %q, want %q", in, got, want)
		}
	}
}
