package handlers_test

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/crypto"
)

// mustKR builds an AES keyring from a hex test key. Tests that construct a
// config.Config literal (rather than via config.Load) don't get a populated
// provider keyring, so they wrap their raw key with this to feed the
// keyring-based service constructors (V2-450).
func mustKR(t *testing.T, hexKey string) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyring(hexKey)
	if err != nil {
		t.Fatalf("build test keyring: %v", err)
	}
	return kr
}
