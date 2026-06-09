package worker

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/crypto"
)

// mustKR builds an AES keyring from a hex test key. Worker tests construct a
// config.Config literal rather than via config.Load, so they wrap their raw key
// with this to feed the keyring-based service constructors (V2-450).
func mustKR(t *testing.T, hexKey string) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyring(hexKey)
	if err != nil {
		t.Fatalf("build test keyring: %v", err)
	}
	return kr
}
