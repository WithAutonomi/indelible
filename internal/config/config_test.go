package config

import (
	"os"
	"path/filepath"
	"testing"
)

// setRequiredSecrets sets the two secrets Load() refuses to start without, so
// tests can focus on the field under test.
func setRequiredSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("INDELIBLE_JWT_SECRET", "test-secret-at-least-32-bytes-long-xx")
	t.Setenv("INDELIBLE_WALLET_ENCRYPTION_KEY", "1111111111111111111111111111111111111111111111111111111111111111")
}

func TestLoad_AdminSeedFromEnv(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INDELIBLE_ADMIN_EMAIL", "Boss@Example.COM")
	t.Setenv("INDELIBLE_ADMIN_PASSWORD", "hunter2hunter2")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AdminEmail != "boss@example.com" {
		t.Errorf("AdminEmail = %q, want normalized lowercase", cfg.AdminEmail)
	}
	if cfg.AdminPassword != "hunter2hunter2" {
		t.Errorf("AdminPassword = %q, want hunter2hunter2", cfg.AdminPassword)
	}
}

func TestLoad_AdminPasswordFileTakesPrecedence(t *testing.T) {
	setRequiredSecrets(t)

	dir := t.TempDir()
	pwFile := filepath.Join(dir, "admin_pw")
	// Trailing newline must be trimmed (typical of `echo > file` / mounted secrets).
	if err := os.WriteFile(pwFile, []byte("file-password\n"), 0o600); err != nil {
		t.Fatalf("write pw file: %v", err)
	}

	t.Setenv("INDELIBLE_ADMIN_EMAIL", "boss@example.com")
	t.Setenv("INDELIBLE_ADMIN_PASSWORD", "inline-password")
	t.Setenv("INDELIBLE_ADMIN_PASSWORD_FILE", pwFile)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AdminPassword != "file-password" {
		t.Errorf("AdminPassword = %q, want file-password (file should win and be trimmed)", cfg.AdminPassword)
	}
}

func TestLoad_AdminPasswordFileMissing(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INDELIBLE_ADMIN_PASSWORD_FILE", filepath.Join(t.TempDir(), "does-not-exist"))

	if _, err := Load(""); err == nil {
		t.Fatal("expected Load to fail when INDELIBLE_ADMIN_PASSWORD_FILE points at a missing file")
	}
}

func TestLoad_JWTSecretTooShort(t *testing.T) {
	t.Setenv("INDELIBLE_WALLET_ENCRYPTION_KEY", "1111111111111111111111111111111111111111111111111111111111111111")
	t.Setenv("INDELIBLE_JWT_SECRET", "short-secret") // 12 bytes, below the 32-byte floor

	if _, err := Load(""); err == nil {
		t.Fatal("expected Load to fail when jwt_secret is shorter than 32 bytes")
	}
}

func TestLoad_JWTSecretAtFloorAccepted(t *testing.T) {
	t.Setenv("INDELIBLE_WALLET_ENCRYPTION_KEY", "1111111111111111111111111111111111111111111111111111111111111111")
	t.Setenv("INDELIBLE_JWT_SECRET", "0123456789abcdef0123456789abcdef") // exactly 32 bytes

	if _, err := Load(""); err != nil {
		t.Fatalf("Load rejected a 32-byte jwt_secret: %v", err)
	}
}
