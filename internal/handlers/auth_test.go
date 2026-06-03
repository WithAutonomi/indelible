package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

type fakeAntdInfo struct{ h *antd.HealthStatus }

func (f fakeAntdInfo) AntdInfo() *antd.HealthStatus { return f.h }

// seedAdminEmail / seedAdminPassword are the conventional bootstrap-admin
// credentials every test relies on: registerAndGetToken with this email logs
// in (the user is pre-seeded as admin) rather than registering.
const (
	seedAdminEmail    = "admin@test.com"
	seedAdminPassword = "password123"
)

func setupTestRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          seedAdminEmail,
		AdminPassword:       seedAdminPassword,
	}

	db := dbtest.OpenDB(t)

	// Seed the bootstrap admin so tests can obtain an admin token by
	// "registering" admin@test.com — registerAndGetToken falls back to login
	// when the user already exists.
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	// Self-registration is off by default; the suite exercises /auth/register
	// to create ordinary (read) users, so enable it. Inserted directly to skip
	// the config_audit row (changed_by would FK-violate with no acting user).
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('registration_enabled', 'true')`); err != nil {
		t.Fatalf("enable registration: %v", err)
	}

	return handlers.NewRouter(cfg, db, nil)
}

func TestRegisterAndLogin(t *testing.T) {
	router := setupTestRouter(t)

	// Register an ordinary user (registration is enabled in the test router).
	// Self-registered users get read-only access — admin is never granted via
	// self-registration; it comes from the bootstrap seed.
	regBody, _ := json.Marshal(map[string]string{
		"email":      "newuser@test.com",
		"password":   "password123",
		"first_name": "Test",
		"last_name":  "User",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("register: got %d, body: %s", w.Code, w.Body.String())
	}

	var regResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &regResp)
	if regResp["token"] == nil || regResp["token"] == "" {
		t.Fatal("register: missing token")
	}
	user := regResp["user"].(map[string]any)
	if user["permissions"] != "read" {
		t.Errorf("self-registered user should be read, got %s", user["permissions"])
	}
	if user["email"] != "newuser@test.com" {
		t.Errorf("email = %s, want newuser@test.com", user["email"])
	}

	// Login with same credentials
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "newuser@test.com",
		"password": "password123",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login: got %d, body: %s", w.Code, w.Body.String())
	}

	var loginResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)
	if token == "" {
		t.Fatal("login: missing token")
	}

	// Use token to get profile
	req = httptest.NewRequest("GET", "/api/v2/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("profile: got %d, body: %s", w.Code, w.Body.String())
	}

	var profile map[string]any
	json.Unmarshal(w.Body.Bytes(), &profile)
	if profile["first_name"] != "Test" {
		t.Errorf("first_name = %s, want Test", profile["first_name"])
	}
}

// setupTestRouterNoReg builds a router on a fresh DB with the bootstrap admin
// seeded but self-registration left at its default (disabled). Used to assert
// the registration gate.
func setupTestRouterNoReg(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          seedAdminEmail,
		AdminPassword:       seedAdminPassword,
	}
	db := dbtest.OpenDB(t)
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return handlers.NewRouter(cfg, db, nil)
}

// TestRegistrationDisabledByDefault asserts the core fix: with registration
// left at its default, POST /auth/register is rejected with 403.
func TestRegistrationDisabledByDefault(t *testing.T) {
	router := setupTestRouterNoReg(t)

	body, _ := json.Marshal(map[string]string{
		"email": "stranger@test.com", "password": "password123",
		"first_name": "S", "last_name": "T",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("register with registration disabled: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestSeededAdminCanLogIn asserts the bootstrap admin created by SeedAdmin can
// authenticate and is recognised as admin — even with registration disabled.
func TestSeededAdminCanLogIn(t *testing.T) {
	router := setupTestRouterNoReg(t)

	loginBody, _ := json.Marshal(map[string]string{
		"email": seedAdminEmail, "password": seedAdminPassword,
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("seed admin login: got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	user := resp["user"].(map[string]any)
	if user["permissions"] != "admin" {
		t.Errorf("seeded user permissions = %v, want admin", user["permissions"])
	}
}

// TestLoginSetsCookies asserts the V2-366 Phase 1 contract: a successful
// login lands both the HttpOnly session cookie carrying the JWT and the
// non-HttpOnly csrf_token cookie the SPA reads for double-submit defence.
func TestLoginSetsSessionAndCSRFCookies(t *testing.T) {
	router := setupTestRouter(t)

	regBody, _ := json.Marshal(map[string]string{
		"email": "cookies@test.com", "password": "password123",
		"first_name": "C", "last_name": "K",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d", w.Code)
	}

	var sawSession, sawCSRF bool
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case "session":
			sawSession = true
			if !c.HttpOnly {
				t.Errorf("session cookie must be HttpOnly")
			}
			if c.Value == "" {
				t.Errorf("session cookie must carry a value")
			}
		case "csrf_token":
			sawCSRF = true
			if c.HttpOnly {
				t.Errorf("csrf_token cookie must NOT be HttpOnly (SPA must read it)")
			}
			if c.Value == "" {
				t.Errorf("csrf_token cookie must carry a value")
			}
		}
	}
	if !sawSession {
		t.Error("register did not set session cookie")
	}
	if !sawCSRF {
		t.Error("register did not set csrf_token cookie")
	}

	// Same assertions on Login.
	loginBody, _ := json.Marshal(map[string]string{
		"email": "cookies@test.com", "password": "password123",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}

	sawSession, sawCSRF = false, false
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			sawSession = true
		}
		if c.Name == "csrf_token" {
			sawCSRF = true
		}
	}
	if !sawSession || !sawCSRF {
		t.Errorf("login cookies: session=%v csrf=%v", sawSession, sawCSRF)
	}
}

// TestCSRFEnforcedOnCookieMutation asserts the V2-366 Phase 3 contract:
// a cookie-authenticated mutation without a matching X-CSRF-Token header
// is rejected with 403, and the same request with the matching header
// succeeds. Bearer callers remain exempt — also verified.
func TestCSRFEnforcedOnCookieMutation(t *testing.T) {
	router := setupTestRouter(t)

	// Register to seed a user + harvest cookies.
	regBody, _ := json.Marshal(map[string]string{
		"email": "csrf@test.com", "password": "password123",
		"first_name": "C", "last_name": "S",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d", w.Code)
	}

	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case "session":
			sessionCookie = c
		case "csrf_token":
			csrfCookie = c
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("missing cookies: session=%v csrf=%v", sessionCookie, csrfCookie)
	}

	// Pick a real mutating endpoint under the authenticated group:
	// PUT /api/v2/me requires both auth (via cookie OR Bearer) and a
	// matching CSRF header when authed via cookie.
	updateBody, _ := json.Marshal(map[string]string{
		"first_name": "Updated",
		"last_name":  "Name",
	})

	// 1. Cookie auth without X-CSRF-Token → 403.
	req = httptest.NewRequest("PUT", "/api/v2/me", bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cookie mutation without CSRF header: got %d, want 403; body=%s", w.Code, w.Body.String())
	}

	// 2. Cookie auth with mismatched X-CSRF-Token → 403.
	req = httptest.NewRequest("PUT", "/api/v2/me", bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "wrong-token")
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cookie mutation with bad CSRF header: got %d, want 403", w.Code)
	}

	// 3. Cookie auth with matching X-CSRF-Token → 200.
	req = httptest.NewRequest("PUT", "/api/v2/me", bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("cookie mutation with matching CSRF header: got %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// 4. Bearer auth without any CSRF header → 200. Bearer callers are
	//    exempt from CSRF by design.
	var regResp map[string]any
	// Login again to harvest a Bearer-usable JWT (response body still
	// includes it for backward-compat with API consumers).
	loginBody, _ := json.Marshal(map[string]string{
		"email": "csrf@test.com", "password": "password123",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &regResp)
	token := regResp["token"].(string)

	req = httptest.NewRequest("PUT", "/api/v2/me", bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Bearer mutation without CSRF header: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	router := setupTestRouter(t)

	body, _ := json.Marshal(map[string]string{
		"email": "dup@test.com", "password": "password123",
		"first_name": "A", "last_name": "B",
	})

	// First register
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first register: %d", w.Code)
	}

	// Duplicate
	req = httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate register: got %d, want 409", w.Code)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	router := setupTestRouter(t)

	// Register
	regBody, _ := json.Marshal(map[string]string{
		"email": "user@test.com", "password": "correctpass",
		"first_name": "A", "last_name": "B",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Login with wrong password
	loginBody, _ := json.Marshal(map[string]string{
		"email": "user@test.com", "password": "wrongpass",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: got %d, want 401", w.Code)
	}
}

func TestSecondUserGetsReadPermission(t *testing.T) {
	router := setupTestRouter(t)

	// First user (admin)
	body1, _ := json.Marshal(map[string]string{
		"email": "first@test.com", "password": "password123",
		"first_name": "First", "last_name": "User",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body1))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Second user (read)
	body2, _ := json.Marshal(map[string]string{
		"email": "second@test.com", "password": "password123",
		"first_name": "Second", "last_name": "User",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body2))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("second register: %d, %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	user := resp["user"].(map[string]any)
	if user["permissions"] != "read" {
		t.Errorf("second user permissions = %s, want read", user["permissions"])
	}
}

func TestHealthEndpoint(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health: got %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Indelible's own version is always reported, even without a managed antd.
	if _, ok := resp["version"]; !ok {
		t.Errorf("expected version field in /health response, got %v", resp)
	}
	// With nil AntdInfoProvider, antd_* diagnostic fields stay unset rather
	// than emitting confusing zero values.
	for _, k := range []string{"antd_version", "antd_evm_network", "antd_uptime_seconds"} {
		if _, ok := resp[k]; ok {
			t.Errorf("unmanaged antd should leave %q unset, got %v", k, resp[k])
		}
	}
}

func TestHealthEndpointSurfacesAntdInfo(t *testing.T) {
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	db := dbtest.OpenDB(t)

	provider := fakeAntdInfo{h: &antd.HealthStatus{
		OK:                  true,
		Network:             "default",
		Version:             "0.4.0",
		EvmNetwork:          "arbitrum-one",
		UptimeSeconds:       99,
		BuildCommit:         "deadbeef1234",
		PaymentTokenAddress: "0xtoken",
		PaymentVaultAddress: "0xvault",
	}}
	router := handlers.NewRouter(cfg, db, provider)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["antd_version"] != "0.4.0" {
		t.Errorf("antd_version = %v, want 0.4.0", resp["antd_version"])
	}
	if resp["antd_evm_network"] != "arbitrum-one" {
		t.Errorf("antd_evm_network = %v, want arbitrum-one", resp["antd_evm_network"])
	}
	if v, _ := resp["antd_uptime_seconds"].(float64); v != 99 {
		t.Errorf("antd_uptime_seconds = %v, want 99", resp["antd_uptime_seconds"])
	}
	if resp["antd_build_commit"] != "deadbeef1234" {
		t.Errorf("antd_build_commit = %v, want deadbeef1234", resp["antd_build_commit"])
	}
	if resp["antd_payment_token_address"] != "0xtoken" || resp["antd_payment_vault_address"] != "0xvault" {
		t.Errorf("antd_payment_*_address mismatch: %v", resp)
	}
}
