package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// V2-774 regression: audit and file-access rows must record the client IP as
// resolved by ResolveClientIP (X-Forwarded-For honored only from a trusted
// proxy), not the raw socket RemoteAddr — behind a reverse proxy the raw
// value is the proxy, which makes the logs useless for attribution.
func TestAuditEventsRecordResolvedClientIP(t *testing.T) {
	db := dbtest.OpenDB(t)
	logSvc := services.NewLogService(db)

	handler := middleware.ResolveClientIP([]string{"10.0.0.0/8"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auditEvent(r, logSvc, "test_audit_ip", "info", nil, "d")
			fileAccessEvent(r, logSvc, "test_file_ip", "info", nil, "d")
		}))

	serve := func(remoteAddr, xff string) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = remoteAddr
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	lastIP := func(table, eventType string) string {
		var ip string
		if err := db.QueryRow(
			"SELECT ip_address FROM "+table+" WHERE event_type = ? ORDER BY id DESC LIMIT 1", eventType,
		).Scan(&ip); err != nil {
			t.Fatalf("read %s row: %v", table, err)
		}
		return ip
	}

	// Behind the trusted proxy: rows carry the forwarded client, not the proxy.
	serve("10.1.2.3:5555", "203.0.113.9, 10.1.2.3")
	if ip := lastIP("audit_log", "test_audit_ip"); ip != "203.0.113.9" {
		t.Fatalf("audit ip = %q, want forwarded client 203.0.113.9", ip)
	}
	if ip := lastIP("file_access_log", "test_file_ip"); ip != "203.0.113.9" {
		t.Fatalf("file-access ip = %q, want forwarded client 203.0.113.9", ip)
	}

	// Direct (untrusted) client: the forged header is ignored and the port
	// noise of RemoteAddr is stripped.
	serve("198.51.100.7:2222", "203.0.113.9")
	if ip := lastIP("audit_log", "test_audit_ip"); ip != "198.51.100.7" {
		t.Fatalf("audit ip = %q, want socket peer 198.51.100.7", ip)
	}
	if ip := lastIP("file_access_log", "test_file_ip"); ip != "198.51.100.7" {
		t.Fatalf("file-access ip = %q, want socket peer 198.51.100.7", ip)
	}
}
