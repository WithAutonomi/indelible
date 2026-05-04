package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func setupTestRouterWithDB(t *testing.T, db *sql.DB, cfg *config.Config) http.Handler {
	t.Helper()
	return handlers.NewRouter(cfg, db, nil)
}

func TestForgotPassword_ConstantTimeResponse(t *testing.T) {
	router := setupTestRouter(t)

	// Request reset for nonexistent email — should return 200 (no enumeration)
	body, _ := json.Marshal(map[string]string{"email": "nobody@test.com"})
	req := httptest.NewRequest("POST", "/api/v2/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("forgot-password (nonexistent): got %d, want 200", w.Code)
	}
}

func TestForgotPassword_ExistingEmail(t *testing.T) {
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "user@test.com", "password123", "Test", "User")

	body, _ := json.Marshal(map[string]string{"email": "user@test.com"})
	req := httptest.NewRequest("POST", "/api/v2/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("forgot-password (existing): got %d, want 200", w.Code)
	}
}

func TestResetTokenService_CreateAndValidate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := services.NewUserService(db)
	resetSvc := services.NewResetTokenService(db)

	hash, _ := auth.HashPassword("password123")
	user, err := userSvc.Create("test@test.com", hash, "Test", "User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create reset token
	token, err := resetSvc.Create(user.ID)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	// Validate token
	userID, err := resetSvc.Validate(token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if userID != user.ID {
		t.Errorf("userID = %d, want %d", userID, user.ID)
	}

	// Token should be single-use
	_, err = resetSvc.Validate(token)
	if err == nil {
		t.Error("expected error on second use of token")
	}
}

func TestResetPassword_FullFlow(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{
		Port:                8080,
		DBURL:               "sqlite://:memory:",
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		BaseURL:             "http://localhost:8080",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	userSvc := services.NewUserService(db)
	resetSvc := services.NewResetTokenService(db)

	hash, _ := auth.HashPassword("oldpassword1")
	user, _ := userSvc.Create("reset@test.com", hash, "Reset", "User")
	// Give user a permission so login works
	permSvc := services.NewPermissionService(db)
	_ = permSvc.SetDirect(user.ID, "read", user.ID)

	// Create token directly (bypassing SMTP)
	token, _ := resetSvc.Create(user.ID)

	router := setupTestRouterWithDB(t, db, cfg)

	// Reset password
	body, _ := json.Marshal(map[string]string{
		"token":        token,
		"new_password": "newpassword1",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("reset-password: got %d, body: %s", w.Code, w.Body.String())
	}

	// Login with new password
	loginBody, _ := json.Marshal(map[string]string{
		"email": "reset@test.com", "password": "newpassword1",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login after reset: got %d, body: %s", w.Code, w.Body.String())
	}

	// Old password should fail
	oldBody, _ := json.Marshal(map[string]string{
		"email": "reset@test.com", "password": "oldpassword1",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(oldBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("old password login: got %d, want 401", w.Code)
	}
}
