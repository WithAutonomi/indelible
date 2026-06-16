package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// V2-518: a reader replica (workers off, no wallet key) must refuse to encrypt
// new wallet/OIDC secrets — otherwise it would seal them under the placeholder
// key into the shared DB, which the writer (real key) could not decrypt. The
// wallet/OIDC *create* + *update* handlers return 503 on such an instance.
func TestAdminCreateWallet_ReaderWithoutKeyReturns503(t *testing.T) {
	t.Setenv("INDELIBLE_JWT_SECRET", "test-secret-for-jwt-signing-1234567890")
	t.Setenv("INDELIBLE_WORKERS_ENABLED", "false")
	t.Setenv("INDELIBLE_ADMIN_EMAIL", seedAdminEmail)
	t.Setenv("INDELIBLE_ADMIN_PASSWORD", seedAdminPassword)
	// Intentionally no INDELIBLE_WALLET_ENCRYPTION_KEY → reader role.

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load reader cfg: %v", err)
	}
	if cfg.WalletKeyConfigured() {
		t.Fatal("expected WalletKeyConfigured()=false on a key-less reader")
	}

	db := dbtest.OpenDB(t)
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	router := handlers.NewRouter(cfg, db, nil)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")

	body, _ := json.Marshal(map[string]string{
		"name":        "should-be-refused",
		"private_key": "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	})
	req := httptest.NewRequest("POST", "/api/v2/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("create wallet on key-less reader: got %d, want 503; body: %s", w.Code, w.Body.String())
	}
}
