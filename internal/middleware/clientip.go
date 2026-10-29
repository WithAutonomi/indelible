package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type clientIPKey struct{}

// ResolveClientIP resolves the real client IP once per request — via ClientIP,
// so X-Forwarded-For is honored only from a trusted proxy — and stashes it in
// the request context for consumers like the audit and file-access logs
// (V2-774). It deliberately does NOT rewrite r.RemoteAddr the way chi's
// removed RealIP middleware did (GO-2026-5777): RemoteAddr stays the socket
// peer, and anything wanting the client identity must ask for the resolved
// value explicitly via RequestClientIP.
func ResolveClientIP(trustedProxies []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), clientIPKey{}, ClientIP(r, trustedProxies))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestClientIP returns the client IP resolved by ResolveClientIP. When the
// middleware is not mounted (tests, bare handlers) it falls back to the bare
// host of RemoteAddr — never the forgeable X-Forwarded-For.
func RequestClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}
	return remoteIP
}

// ClientIP extracts the client IP address from a request, respecting
// X-Forwarded-For only if the direct connection comes from a trusted proxy.
// If trustedProxies is empty, X-Forwarded-For is never used (safe default).
func ClientIP(r *http.Request, trustedProxies []string) string {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	if len(trustedProxies) == 0 {
		return remoteIP
	}

	if !isTrustedProxy(remoteIP, trustedProxies) {
		return remoteIP
	}

	// Trust the first (leftmost) IP in X-Forwarded-For from a trusted proxy
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.SplitN(fwd, ",", 2)
		clientIP := strings.TrimSpace(parts[0])
		if clientIP != "" {
			return clientIP
		}
	}

	return remoteIP
}

func isTrustedProxy(ip string, trusted []string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	for _, t := range trusted {
		// Check CIDR range
		if strings.Contains(t, "/") {
			_, cidr, err := net.ParseCIDR(t)
			if err == nil && cidr.Contains(parsed) {
				return true
			}
			continue
		}
		// Check exact IP
		if t == ip {
			return true
		}
	}
	return false
}
