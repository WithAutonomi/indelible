package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func resolveThrough(t *testing.T, trustedProxies []string, remoteAddr, xff string) string {
	t.Helper()
	var got string
	h := ResolveClientIP(trustedProxies)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestClientIP(r)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestResolveClientIP_TrustedProxyHonorsForwardedFor(t *testing.T) {
	got := resolveThrough(t, []string{"10.0.0.0/8"}, "10.1.2.3:5555", "203.0.113.9, 10.1.2.3")
	if got != "203.0.113.9" {
		t.Fatalf("resolved %q, want the forwarded client 203.0.113.9", got)
	}
}

func TestResolveClientIP_UntrustedPeerIgnoresForwardedFor(t *testing.T) {
	got := resolveThrough(t, []string{"10.0.0.0/8"}, "198.51.100.7:2222", "203.0.113.9")
	if got != "198.51.100.7" {
		t.Fatalf("resolved %q, want the socket peer 198.51.100.7 (X-Forwarded-For is forgeable)", got)
	}
}

func TestResolveClientIP_NoProxiesConfigured(t *testing.T) {
	got := resolveThrough(t, nil, "198.51.100.7:2222", "203.0.113.9")
	if got != "198.51.100.7" {
		t.Fatalf("resolved %q, want 198.51.100.7 (no trusted proxies -> never trust the header)", got)
	}
}

func TestRequestClientIP_FallbackWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.10:4444"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := RequestClientIP(req); got != "192.0.2.10" {
		t.Fatalf("fallback resolved %q, want the bare RemoteAddr host 192.0.2.10", got)
	}
}
