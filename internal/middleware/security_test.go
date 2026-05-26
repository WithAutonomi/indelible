package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"Referrer-Policy":       "strict-origin-when-cross-origin",
	}

	for header, want := range expected {
		got := w.Header().Get(header)
		if got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	// These should be present but we just check non-empty
	for _, header := range []string{"Content-Security-Policy", "Permissions-Policy"} {
		if w.Header().Get(header) == "" {
			t.Errorf("missing security header: %s", header)
		}
	}
}

func TestSwaggerCSP_AllowsInlineScripts(t *testing.T) {
	// Chain global SecurityHeaders then SwaggerCSP — mirrors the router. The
	// per-route override must take precedence.
	handler := SecurityHeaders()(SwaggerCSP()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/api/docs/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if !contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("expected swagger CSP to allow inline scripts, got %q", csp)
	}
	if !contains(csp, "frame-ancestors 'none'") {
		t.Errorf("swagger CSP must keep frame-ancestors 'none' so docs page can't be embedded, got %q", csp)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
