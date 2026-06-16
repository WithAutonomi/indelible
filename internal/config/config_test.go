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

func TestLoad_WorkersEnabledDefaultsTrue(t *testing.T) {
	setRequiredSecrets(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WorkersEnabled {
		t.Error("WorkersEnabled = false, want true by default (all-in-one / writer role)")
	}
}

func TestLoad_WorkersEnabledFalseDisables(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INDELIBLE_WORKERS_ENABLED", "false")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WorkersEnabled {
		t.Error("WorkersEnabled = true, want false when INDELIBLE_WORKERS_ENABLED=false (reader role)")
	}
}

func TestLoad_ReaderBootsWithoutWalletKey(t *testing.T) {
	// Reader role (workers off): wallet key is not required — readers never
	// decrypt a wallet or OIDC secret (V2-518).
	t.Setenv("INDELIBLE_JWT_SECRET", "test-secret-at-least-32-bytes-long-xx")
	t.Setenv("INDELIBLE_WORKERS_ENABLED", "false")
	// Intentionally no INDELIBLE_WALLET_ENCRYPTION_KEY.

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("reader Load without wallet key should succeed, got: %v", err)
	}
	if cfg.WorkersEnabled {
		t.Error("WorkersEnabled = true, want false")
	}
	// A (placeholder) wallet keyring is still built so consumers don't nil-deref;
	// it's never used to protect real data on a reader.
	if cfg.WalletKeyring() == nil {
		t.Error("WalletKeyring() = nil, want a non-nil keyring")
	}
}

func TestLoad_WalletKeyRequiredWhenWorkersEnabled(t *testing.T) {
	// The writer / all-in-one role (workers on, the default) still requires it.
	t.Setenv("INDELIBLE_JWT_SECRET", "test-secret-at-least-32-bytes-long-xx")
	// No wallet key, workers default to enabled.

	if _, err := Load(""); err == nil {
		t.Fatal("expected Load to fail without a wallet key when workers are enabled")
	}
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

func TestLoad_JWTSecretsPrevious(t *testing.T) {
	setRequiredSecrets(t)
	// Comma-separated, with whitespace and a trailing empty entry to trim.
	t.Setenv("INDELIBLE_JWT_SECRET_PREVIOUS", " old-secret-key-at-least-32-chars-long , another-old-key-at-least-32-chars-x ,")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"old-secret-key-at-least-32-chars-long", "another-old-key-at-least-32-chars-x"}
	if len(cfg.JWTSecretsPrevious) != len(want) {
		t.Fatalf("JWTSecretsPrevious = %v, want %v", cfg.JWTSecretsPrevious, want)
	}
	for i, s := range want {
		if cfg.JWTSecretsPrevious[i] != s {
			t.Errorf("JWTSecretsPrevious[%d] = %q, want %q", i, cfg.JWTSecretsPrevious[i], s)
		}
	}
}

func TestLoad_JWTSecretPreviousTooShort(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INDELIBLE_JWT_SECRET_PREVIOUS", "short-old-secret") // below the 32-byte floor

	if _, err := Load(""); err == nil {
		t.Fatal("expected Load to fail when a previous jwt secret is shorter than 32 bytes")
	}
}

func TestLoad_SecretFilesTakePrecedence(t *testing.T) {
	// _FILE variants (Docker/K8s secrets) for the JWT secret and wallet key win
	// over the inline env vars and are whitespace-trimmed (V2-450).
	dir := t.TempDir()
	jwtFile := filepath.Join(dir, "jwt")
	walletFile := filepath.Join(dir, "wallet")
	if err := os.WriteFile(jwtFile, []byte("file-jwt-secret-at-least-32-bytes-xx\n"), 0o600); err != nil {
		t.Fatalf("write jwt file: %v", err)
	}
	if err := os.WriteFile(walletFile, []byte("2222222222222222222222222222222222222222222222222222222222222222\n"), 0o600); err != nil {
		t.Fatalf("write wallet file: %v", err)
	}
	t.Setenv("INDELIBLE_JWT_SECRET", "inline-jwt-secret-at-least-32-bytes-x")
	t.Setenv("INDELIBLE_WALLET_ENCRYPTION_KEY", "1111111111111111111111111111111111111111111111111111111111111111")
	t.Setenv("INDELIBLE_JWT_SECRET_FILE", jwtFile)
	t.Setenv("INDELIBLE_WALLET_ENCRYPTION_KEY_FILE", walletFile)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWTSecret != "file-jwt-secret-at-least-32-bytes-xx" {
		t.Errorf("JWTSecret = %q, want the file value (trimmed)", cfg.JWTSecret)
	}
	if cfg.WalletEncryptionKey != "2222222222222222222222222222222222222222222222222222222222222222" {
		t.Errorf("WalletEncryptionKey = %q, want the file value (trimmed)", cfg.WalletEncryptionKey)
	}
	// The provider keyrings reflect the file-sourced material.
	if cfg.JWTKeyring().Primary() != cfg.JWTSecret {
		t.Error("JWT keyring primary should match the file-sourced secret")
	}
}

func TestLoad_WalletKeysPreviousFeedKeyring(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INDELIBLE_WALLET_ENCRYPTION_KEY_PREVIOUS",
		" 2222222222222222222222222222222222222222222222222222222222222222 ,")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prev := cfg.WalletKeyring().Previous()
	if len(prev) != 1 || prev[0] != "2222222222222222222222222222222222222222222222222222222222222222" {
		t.Errorf("wallet keyring Previous() = %v, want the one former key", prev)
	}
}

func TestLoad_UnknownSecretsBackendRejected(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INDELIBLE_SECRETS_BACKEND", "vault")

	if _, err := Load(""); err == nil {
		t.Fatal("expected Load to fail for an unimplemented secrets backend")
	}
}

func TestApplyNetworkPreset_LocalAliasesCustom(t *testing.T) {
	// "local" is antd's term for a devnet; Indelible should accept it as an
	// alias for "custom" rather than rejecting it, and fold it to the canonical
	// value so it leaves the EVM fields for upload-time auto-population.
	c := &Config{Network: NetworkLocal}
	if err := c.ApplyNetworkPreset(); err != nil {
		t.Fatalf("ApplyNetworkPreset rejected network %q: %v", NetworkLocal, err)
	}
	if c.Network != NetworkCustom {
		t.Errorf("Network = %q, want it normalized to %q", c.Network, NetworkCustom)
	}
	if c.EvmRPCURL != "" || c.EvmTokenAddress != "" {
		t.Errorf("EVM fields should be left untouched for a local/custom network, got rpc=%q token=%q",
			c.EvmRPCURL, c.EvmTokenAddress)
	}
}

func TestApplyNetworkPreset_UnknownRejected(t *testing.T) {
	c := &Config{Network: "no-such-network"}
	if err := c.ApplyNetworkPreset(); err == nil {
		t.Fatal("expected ApplyNetworkPreset to reject an unknown network")
	}
}
