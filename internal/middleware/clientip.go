package middleware

import (
	"net"
	"net/http"
	"strings"
)

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
