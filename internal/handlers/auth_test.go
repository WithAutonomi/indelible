package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/handlers"
)

type fakeAntdInfo struct{ h *antd.HealthStatus }

func (f fakeAntdInfo) AntdInfo() *antd.HealthStatus { return f.h }

func setupTestRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		DBURL:               "sqlite://:memory:",
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	db, err := database.Open(cfg.DBURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return handlers.NewRouter(cfg, db, nil)
}

func TestRegisterAndLogin(t *testing.T) {
	router := setupTestRouter(t)

	// Register first user (should become admin)
	regBody, _ := json.Marshal(map[string]string{
		"email":      "admin@test.com",
		"password":   "password123",
		"first_name": "Test",
		"last_name":  "Admin",
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
	if user["permissions"] != "admin" {
		t.Errorf("first user should be admin, got %s", user["permissions"])
	}
	if user["email"] != "admin@test.com" {
		t.Errorf("email = %s, want admin@test.com", user["email"])
	}

	// Login with same credentials
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "admin@test.com",
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
		DBURL:               "sqlite://:memory:",
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	db, err := database.Open(cfg.DBURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

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
