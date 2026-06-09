package secrets

import (
	"strings"
	"testing"
)

const (
	hexKeyA = "1111111111111111111111111111111111111111111111111111111111111111"
	hexKeyB = "2222222222222222222222222222222222222222222222222222222222222222"
	jwtA    = "this-is-a-sufficiently-long-jwt-secret-aaaa"
	jwtB    = "this-is-a-sufficiently-long-jwt-secret-bbbb"
)

func newTestProvider(t *testing.T) Provider {
	t.Helper()
	p, err := NewProvider(BackendEnv, EnvConfig{
		WalletKey:          hexKeyA,
		WalletKeysPrevious: []string{hexKeyB},
		JWTSecret:          jwtA,
		JWTSecretsPrevious: []string{jwtB},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

func TestNewProvider_DefaultIsEnv(t *testing.T) {
	for _, backend := range []string{"", BackendEnv} {
		if _, err := NewProvider(backend, EnvConfig{WalletKey: hexKeyA, JWTSecret: jwtA}); err != nil {
			t.Errorf("NewProvider(%q): unexpected error %v", backend, err)
		}
	}
}

func TestNewProvider_UnknownBackendErrors(t *testing.T) {
	_, err := NewProvider("vault", EnvConfig{WalletKey: hexKeyA, JWTSecret: jwtA})
	if err == nil {
		t.Fatal("expected error for unimplemented backend")
	}
	if !strings.Contains(err.Error(), "vault") {
		t.Errorf("error should name the backend, got %q", err.Error())
	}
}

func TestNewProvider_BadWalletKeyErrorsAtBuild(t *testing.T) {
	_, err := NewProvider(BackendEnv, EnvConfig{WalletKey: "deadbeef", JWTSecret: jwtA})
	if err == nil {
		t.Fatal("expected error for malformed wallet key")
	}
	if !strings.Contains(err.Error(), WalletEncryption) {
		t.Errorf("error should name the wallet keyring, got %q", err.Error())
	}
}

func TestProvider_WalletKeyringHasHistory(t *testing.T) {
	p := newTestProvider(t)
	kr, err := p.Keyring(WalletEncryption)
	if err != nil {
		t.Fatalf("Keyring(%s): %v", WalletEncryption, err)
	}
	if kr.Primary() != hexKeyA {
		t.Errorf("primary = %q, want %q", kr.Primary(), hexKeyA)
	}
	if prev := kr.Previous(); len(prev) != 1 || prev[0] != hexKeyB {
		t.Errorf("previous = %v, want [%s]", prev, hexKeyB)
	}
	// A value encrypted under the primary round-trips.
	env, err := kr.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if got, _ := kr.Decrypt(env); got != "secret" {
		t.Errorf("round-trip = %q, want %q", got, "secret")
	}
}

func TestProvider_JWTKeyringPrimaryAndPrevious(t *testing.T) {
	p := newTestProvider(t)
	kr, err := p.Keyring(JWT)
	if err != nil {
		t.Fatalf("Keyring(%s): %v", JWT, err)
	}
	if kr.Primary() != jwtA {
		t.Errorf("primary = %q, want %q", kr.Primary(), jwtA)
	}
	if prev := kr.Previous(); len(prev) != 1 || prev[0] != jwtB {
		t.Errorf("previous = %v, want [%s]", prev, jwtB)
	}
}

func TestProvider_UnknownNameErrors(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.Keyring("nope"); err == nil {
		t.Error("expected error for unconfigured keyring name")
	}
}
