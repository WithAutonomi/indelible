package services

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/crypto"
)

// mustKR builds an AES keyring from a hex test key, failing the test on a bad
// key. It mirrors how production builds the wallet/OIDC keyring via the secrets
// provider (V2-450), letting tests pass a *crypto.Keyring where they used to
// pass a raw hex string.
func mustKR(t *testing.T, hexKey string, previous ...string) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyring(hexKey, previous...)
	if err != nil {
		t.Fatalf("build test keyring: %v", err)
	}
	return kr
}
