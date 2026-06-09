package services

import (
	"fmt"

	"github.com/WithAutonomi/indelible/internal/crypto"
	"github.com/WithAutonomi/indelible/internal/database"
)

// RotationResult reports how many encrypted rows were re-encrypted under the new
// key.
type RotationResult struct {
	Wallets       int
	OIDCProviders int
	OldKeyID      string
	NewKeyID      string
}

// RotateEncryptionKey re-encrypts every secret stored under the wallet/OIDC
// encryption key — wallet private keys (wallets.encrypted_key) and OIDC client
// secrets (oidc_providers.client_secret) — from oldHexKey to newHexKey, in a
// single transaction (all-or-nothing).
//
// Both keys are needed because the running service only ever holds one. The
// decrypt keyring carries both (plus legacy/un-tagged rows → old), so a mix of
// old, already-new (interrupted run), and legacy rows all decrypt; everything is
// re-written tagged with the new key's id. That makes the operation safe to
// re-run and leaves no silently-bricked rows if interrupted.
//
// Run it offline (service stopped) via the `rotate-keys` CLI; afterwards set
// INDELIBLE_WALLET_ENCRYPTION_KEY to the new key and restart.
func RotateEncryptionKey(db *database.DB, oldHexKey, newHexKey string) (RotationResult, error) {
	var res RotationResult

	// decKR decrypts old, new, and legacy(→old) rows; encKR writes new-tagged.
	decKR, err := crypto.NewKeyring(oldHexKey, newHexKey)
	if err != nil {
		return res, fmt.Errorf("build decrypt keyring: %w", err)
	}
	encKR, err := crypto.NewKeyring(newHexKey)
	if err != nil {
		return res, fmt.Errorf("build encrypt keyring: %w", err)
	}
	oldID, _ := crypto.KeyID(oldHexKey)
	res.OldKeyID, res.NewKeyID = oldID, encKR.PrimaryID()

	tx, err := db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	// Wallets.
	walletRows, err := tx.Query(`SELECT id, encrypted_key FROM wallets`)
	if err != nil {
		return res, fmt.Errorf("list wallets: %w", err)
	}
	type reenc struct {
		id  int64
		val string
	}
	var wallets []reenc
	for walletRows.Next() {
		var id int64
		var enc string
		if err := walletRows.Scan(&id, &enc); err != nil {
			walletRows.Close()
			return res, err
		}
		plain, err := decKR.Decrypt(enc)
		if err != nil {
			walletRows.Close()
			return res, fmt.Errorf("decrypt wallet %d (key mismatch?): %w", id, err)
		}
		newEnc, err := encKR.Encrypt(plain)
		if err != nil {
			walletRows.Close()
			return res, fmt.Errorf("re-encrypt wallet %d: %w", id, err)
		}
		wallets = append(wallets, reenc{id, newEnc})
	}
	walletRows.Close()
	if err := walletRows.Err(); err != nil {
		return res, err
	}
	for _, w := range wallets {
		if _, err := tx.Exec(`UPDATE wallets SET encrypted_key = ? WHERE id = ?`, w.val, w.id); err != nil {
			return res, fmt.Errorf("update wallet %d: %w", w.id, err)
		}
	}
	res.Wallets = len(wallets)

	// OIDC provider client secrets share the same key.
	provRows, err := tx.Query(`SELECT id, client_secret FROM oidc_providers`)
	if err != nil {
		return res, fmt.Errorf("list oidc providers: %w", err)
	}
	var providers []reenc
	for provRows.Next() {
		var id int64
		var enc string
		if err := provRows.Scan(&id, &enc); err != nil {
			provRows.Close()
			return res, err
		}
		plain, err := decKR.Decrypt(enc)
		if err != nil {
			provRows.Close()
			return res, fmt.Errorf("decrypt oidc provider %d (key mismatch?): %w", id, err)
		}
		newEnc, err := encKR.Encrypt(plain)
		if err != nil {
			provRows.Close()
			return res, fmt.Errorf("re-encrypt oidc provider %d: %w", id, err)
		}
		providers = append(providers, reenc{id, newEnc})
	}
	provRows.Close()
	if err := provRows.Err(); err != nil {
		return res, err
	}
	for _, p := range providers {
		if _, err := tx.Exec(`UPDATE oidc_providers SET client_secret = ? WHERE id = ?`, p.val, p.id); err != nil {
			return res, fmt.Errorf("update oidc provider %d: %w", p.id, err)
		}
	}
	res.OIDCProviders = len(providers)

	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}
