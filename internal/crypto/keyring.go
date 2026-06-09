package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// keyIDLen is the number of hex chars of SHA-256(key) used as a key's id. 8 hex
// chars (32 bits) is ample to distinguish the handful of keys a deployment
// rotates through, while keeping the stored envelope compact.
const keyIDLen = 8

// KeyID derives a short, stable identifier for a hex-encoded AES key: the first
// keyIDLen hex chars of SHA-256(keyBytes). It depends only on the key, so the
// same key always yields the same id (no registry needed), and it never
// contains ':' — letting it prefix an envelope unambiguously, since hex
// ciphertext never contains ':' either.
func KeyID(hexEncodedKey string) (string, error) {
	b, err := hexKey(hexEncodedKey)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:keyIDLen], nil
}

// Keyring encrypts and decrypts a key-id-tagged envelope:
//
//	"<keyid>:<hex(nonce‖ciphertext)>"
//
// Encrypt always tags with the primary key's id. Decrypt reads the id prefix
// and selects the matching key; a value with NO ':' prefix is treated as legacy
// ciphertext (written before key-ids existed) and decrypted with the primary
// key. Holding more than one key lets a rotation tool decrypt a mix of
// old-key, new-key, and legacy rows in flight — so an interrupted rotation
// leaves every row identifiable and recoverable rather than silently bricked.
//
// This is the seam V2-450 (pluggable secrets backend) will generalize.
type Keyring struct {
	primary string            // key-id used by Encrypt
	keys    map[string]string // key-id -> hex-encoded key
}

// NewKeyring builds a keyring whose primary (encrypt) key is primaryHexKey.
// Any extraHexKeys are added for decryption only (e.g. the old key during a
// rotation). All keys are validated as 32-byte hex.
func NewKeyring(primaryHexKey string, extraHexKeys ...string) (*Keyring, error) {
	pid, err := KeyID(primaryHexKey)
	if err != nil {
		return nil, fmt.Errorf("primary key: %w", err)
	}
	kr := &Keyring{primary: pid, keys: map[string]string{pid: primaryHexKey}}
	for _, hk := range extraHexKeys {
		id, err := KeyID(hk)
		if err != nil {
			return nil, fmt.Errorf("extra key: %w", err)
		}
		kr.keys[id] = hk
	}
	return kr, nil
}

// PrimaryID returns the key-id of the primary (encrypt) key.
func (k *Keyring) PrimaryID() string { return k.primary }

// Encrypt encrypts plaintext under the primary key and returns the tagged
// envelope "<primaryid>:<hexct>".
func (k *Keyring) Encrypt(plaintext string) (string, error) {
	ct, err := Encrypt(k.keys[k.primary], plaintext)
	if err != nil {
		return "", err
	}
	return k.primary + ":" + ct, nil
}

// Decrypt decrypts an envelope produced by Encrypt, or a legacy (un-tagged)
// ciphertext using the primary key.
func (k *Keyring) Decrypt(envelope string) (string, error) {
	if id, payload, found := strings.Cut(envelope, ":"); found {
		key, ok := k.keys[id]
		if !ok {
			return "", fmt.Errorf("no key available for key-id %q", id)
		}
		return Decrypt(key, payload)
	}
	// Legacy ciphertext (no key-id prefix) — written before envelopes existed.
	return Decrypt(k.keys[k.primary], envelope)
}
