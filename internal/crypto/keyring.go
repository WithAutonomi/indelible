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

// rawKeyID derives a key-id from arbitrary secret material (e.g. an HMAC JWT
// signing secret) by hashing its raw bytes rather than hex-decoding first. Used
// by NewKeyringRaw for secrets that are not hex-encoded AES keys. It never
// errors — the error return exists only to share newKeyring's id-function shape
// with KeyID.
func rawKeyID(secret string) (string, error) {
	sum := sha256.Sum256([]byte(secret))
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
// A keyring is also the unit a secrets.Provider hands consumers (V2-450): the
// active (primary) key plus verify/decrypt-only history, addressable by key-id.
// AES consumers use Encrypt/Decrypt; sign/verify consumers (JWT, and the future
// B-2 audit-signing key) use Primary/Previous to sign with the active key and
// verify against the whole set.
type Keyring struct {
	primary string            // key-id used by Encrypt / Primary
	order   []string          // key-ids, primary first then extras in insertion order
	keys    map[string]string // key-id -> key material
}

// NewKeyring builds a keyring whose primary (encrypt) key is primaryHexKey.
// Any extraHexKeys are added for decryption only (e.g. the old key during a
// rotation). All keys are validated as 32-byte hex.
func NewKeyring(primaryHexKey string, extraHexKeys ...string) (*Keyring, error) {
	return newKeyring(KeyID, primaryHexKey, extraHexKeys...)
}

// NewKeyringRaw builds a keyring over arbitrary (non-hex) secret material, such
// as HMAC JWT signing secrets. Key-ids derive from the raw secret bytes, so
// these keyrings are for sign/verify via Primary/Previous; the AES Encrypt and
// Decrypt methods require hex material and will error on a raw keyring.
func NewKeyringRaw(primary string, previous ...string) (*Keyring, error) {
	return newKeyring(rawKeyID, primary, previous...)
}

func newKeyring(idFn func(string) (string, error), primary string, extras ...string) (*Keyring, error) {
	pid, err := idFn(primary)
	if err != nil {
		return nil, fmt.Errorf("primary key: %w", err)
	}
	kr := &Keyring{primary: pid, order: []string{pid}, keys: map[string]string{pid: primary}}
	for _, e := range extras {
		id, err := idFn(e)
		if err != nil {
			return nil, fmt.Errorf("extra key: %w", err)
		}
		if _, dup := kr.keys[id]; dup {
			continue // same material as a key already held — no second entry
		}
		kr.keys[id] = e
		kr.order = append(kr.order, id)
	}
	return kr, nil
}

// PrimaryID returns the key-id of the primary (encrypt/sign) key.
func (k *Keyring) PrimaryID() string { return k.primary }

// Primary returns the material of the active key — the secret to sign new
// tokens with, or the hex key Encrypt uses.
func (k *Keyring) Primary() string { return k.keys[k.primary] }

// Previous returns the verify/decrypt-only history key material in insertion
// order (excluding the primary). Sign/verify consumers try Primary first, then
// each of these, so a token signed under a former secret keeps validating until
// the secret is dropped from the keyring.
func (k *Keyring) Previous() []string {
	prev := make([]string, 0, len(k.order)-1)
	for _, id := range k.order[1:] {
		prev = append(prev, k.keys[id])
	}
	return prev
}

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
