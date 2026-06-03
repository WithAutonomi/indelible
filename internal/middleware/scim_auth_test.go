package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// V2-303: SCIMAuth must accept Okta's "Header Auth" variant which sends
// the API Token field verbatim — no "Bearer " scheme prefix. The scim_
// prefix is the actual token discriminator.

// scimTestEnv builds a router + SCIM token in one place. Mirrors the
// handler-package helper but lives here so we can exercise the middleware
// directly through the chain.
func setupSCIMAuthTest(t *testing.T) (router http.Handler, scimSecret string) {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          "admin@test.com",
		AdminPassword:       "password123",
	}
	db := dbtest.OpenDB(t)
	// Seed the bootstrap admin (self-registration is disabled by default) so we
	// can mint a SCIM token via the admin API, then log in to get its token.
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	router = handlers.NewRouter(cfg, db, nil)

	body, _ := json.Marshal(map[string]string{
		"email": "admin@test.com", "password": "password123",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login admin: got %d", w.Code)
	}
	var regResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &regResp)
	adminToken := regResp["token"].(string)

	// Enable SCIM.
	patchBody, _ := json.Marshal(map[string]string{"scim_enabled": "true"})
	req = httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable scim: got %d", w.Code)
	}

	// Mint a SCIM token.
	tokenBody, _ := json.Marshal(map[string]string{"name": "test-okta"})
	req = httptest.NewRequest("POST", "/api/v2/admin/scim/tokens", bytes.NewReader(tokenBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create scim token: got %d", w.Code)
	}
	var tokResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &tokResp)
	return router, tokResp["secret"].(string)
}

func TestSCIMAuth_AcceptsBearerScheme(t *testing.T) {
	router, secret := setupSCIMAuthTest(t)

	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Bearer scim_<hex>: got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSCIMAuth_AcceptsBareToken(t *testing.T) {
	router, secret := setupSCIMAuthTest(t)

	// V2-303: bare token, no Bearer prefix (Okta Header Auth variant).
	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", secret)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bare scim_<hex>: got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSCIMAuth_RejectsBasic(t *testing.T) {
	router, _ := setupSCIMAuthTest(t)

	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Basic auth: got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid authorization format") {
		t.Errorf("expected invalid-format error, got: %s", w.Body.String())
	}
}

func TestSCIMAuth_RejectsWrongTokenAtValidateStep(t *testing.T) {
	router, _ := setupSCIMAuthTest(t)

	// Format passes (scim_ prefix) but token doesn't exist → fails at Validate.
	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "scim_wrongtoken")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong scim_ token: got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid or revoked SCIM token") {
		t.Errorf("expected validate-step error, got: %s", w.Body.String())
	}
}
