// Package secrets is the seam between application configuration and the key
// material the app signs and encrypts with (V2-450). Consumers depend on the
// Provider interface, which hands out keyrings — an active key plus
// verify/decrypt-only rotation history — rather than bare strings. The default
// backend sources material from process configuration (env / config-file /
// _FILE); a future backend (HashiCorp Vault, a cloud KMS / secret manager) can
// satisfy the same interface without touching any consumer.
package secrets

import (
	"fmt"

	"github.com/WithAutonomi/indelible/internal/crypto"
)

// Logical secret names addressed through a Provider. Adding the audit-signing
// key here later (V2-437 / B-2) is a one-line change plus its source wiring —
// consumers already speak the keyring vocabulary.
const (
	// WalletEncryption is the AES key protecting stored wallet private keys and
	// OIDC client secrets. Its keyring is hex/AES (Encrypt/Decrypt).
	WalletEncryption = "wallet-encryption"
	// JWT is the HMAC secret signing session tokens. Its keyring is raw
	// (Primary/Previous for sign/verify).
	JWT = "jwt"
)

// Provider sources application secrets as keyrings: an active (primary) key for
// new operations plus zero or more verify/decrypt-only history keys, each
// addressable by a stable key-id. This is the abstraction a KMS/Vault backend
// implements without changing consumers.
type Provider interface {
	// Keyring returns the keyring for a logical secret name (e.g.
	// WalletEncryption, JWT). The primary is the active key; Previous() holds
	// the rotation history. Returns an error for an unconfigured name.
	Keyring(name string) (*crypto.Keyring, error)
}

// EnvConfig is the already-resolved secret material the default provider
// assembles into keyrings. Sourcing (env vars, TOML, _FILE) and entropy
// validation are the config package's job; this keeps the secrets package free
// of any dependency on config (avoiding an import cycle).
type EnvConfig struct {
	WalletKey          string   // active hex AES key
	WalletKeysPrevious []string // former hex AES keys, decrypt-only
	JWTSecret          string   // active HMAC secret
	JWTSecretsPrevious []string // former HMAC secrets, verify-only
}

// EnvProvider is the default backend. It holds keyrings built once at load
// time, so Keyring never fails for a configured name at request time.
type EnvProvider struct {
	keyrings map[string]*crypto.Keyring
}

// Keyring implements Provider.
func (p *EnvProvider) Keyring(name string) (*crypto.Keyring, error) {
	kr, ok := p.keyrings[name]
	if !ok {
		return nil, fmt.Errorf("secrets: no keyring configured for %q", name)
	}
	return kr, nil
}

// NewEnvProvider assembles the wallet (AES) and JWT (HMAC) keyrings from c.
// Building validates the underlying material — invalid hex for the wallet key,
// for example — so a misconfiguration is caught at startup, not mid-request.
func NewEnvProvider(c EnvConfig) (*EnvProvider, error) {
	walletKR, err := crypto.NewKeyring(c.WalletKey, c.WalletKeysPrevious...)
	if err != nil {
		return nil, fmt.Errorf("%s keyring: %w", WalletEncryption, err)
	}
	jwtKR, err := crypto.NewKeyringRaw(c.JWTSecret, c.JWTSecretsPrevious...)
	if err != nil {
		return nil, fmt.Errorf("%s keyring: %w", JWT, err)
	}
	return &EnvProvider{keyrings: map[string]*crypto.Keyring{
		WalletEncryption: walletKR,
		JWT:              jwtKR,
	}}, nil
}

// Backend identifiers for NewProvider, selected via INDELIBLE_SECRETS_BACKEND.
const (
	BackendEnv = "env"
)

// NewProvider constructs the Provider for the named backend. The default
// (empty or "env") sources from configuration. Other backends (Vault, cloud
// KMS / secret managers) are not yet implemented — the interface is in place so
// they can be added without touching consumers.
func NewProvider(backend string, env EnvConfig) (Provider, error) {
	switch backend {
	case "", BackendEnv:
		return NewEnvProvider(env)
	default:
		return nil, fmt.Errorf("secrets: backend %q is not implemented; only %q is available", backend, BackendEnv)
	}
}
