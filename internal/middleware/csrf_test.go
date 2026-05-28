package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WithAutonomi/indelible/internal/middleware"
)

func handlerOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func withAuthSource(r *http.Request, src string) *http.Request {
	if src == "" {
		return r
	}
	ctx := context.WithValue(r.Context(), middleware.AuthSourceKey, src)
	return r.WithContext(ctx)
}

func TestCSRF_ReadMethodsAlwaysPass(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/x", nil)
			// No cookie, no header — would otherwise mismatch.
			req = withAuthSource(req, middleware.AuthSourceCookie)
			rec := httptest.NewRecorder()

			middleware.CSRF(true)(handlerOK()).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d", method, rec.Code)
			}
		})
	}
}

func TestCSRF_BearerCallerExempt(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req = withAuthSource(req, middleware.AuthSourceHeader)
	rec := httptest.NewRecorder()

	middleware.CSRF(true)(handlerOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Bearer caller should skip CSRF, got %d", rec.Code)
	}
}

func TestCSRF_NoAuthSource_Exempt(t *testing.T) {
	// Defensive: middleware should only enforce when Authenticate has tagged
	// the request as cookie-authenticated. Anything else passes through so
	// public/auth-free routes are never broken by accidental ordering.
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()

	middleware.CSRF(true)(handlerOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated request should pass through CSRF, got %d", rec.Code)
	}
}

func TestCSRF_CookieMutation_NoHeader_Enforce_Rejects(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: "abc"})
	req = withAuthSource(req, middleware.AuthSourceCookie)
	rec := httptest.NewRecorder()

	middleware.CSRF(true)(handlerOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing header should 403 in enforce mode, got %d", rec.Code)
	}
}

func TestCSRF_CookieMutation_MismatchHeader_Enforce_Rejects(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: "abc"})
	req.Header.Set(middleware.CSRFHeaderName, "xyz")
	req = withAuthSource(req, middleware.AuthSourceCookie)
	rec := httptest.NewRecorder()

	middleware.CSRF(true)(handlerOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched header should 403 in enforce mode, got %d", rec.Code)
	}
}

func TestCSRF_CookieMutation_MatchingHeader_Passes(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: "abc"})
	req.Header.Set(middleware.CSRFHeaderName, "abc")
	req = withAuthSource(req, middleware.AuthSourceCookie)
	rec := httptest.NewRecorder()

	middleware.CSRF(true)(handlerOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("matching header should pass, got %d", rec.Code)
	}
}

func TestCSRF_ReportOnly_MismatchPassesButLogs(t *testing.T) {
	// In report-only mode, mismatches should still allow the request through
	// — only logging changes vs enforce mode. (Log verification is via slog
	// handler in real usage; here we just assert the request passes.)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: "abc"})
	req.Header.Set(middleware.CSRFHeaderName, "wrong")
	req = withAuthSource(req, middleware.AuthSourceCookie)
	rec := httptest.NewRecorder()

	middleware.CSRF(false)(handlerOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("report-only should pass mismatches, got %d", rec.Code)
	}
}
