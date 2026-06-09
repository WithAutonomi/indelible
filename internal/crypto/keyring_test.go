package crypto

import "testing"

const (
	keyA = "1111111111111111111111111111111111111111111111111111111111111111"
	keyB = "2222222222222222222222222222222222222222222222222222222222222222"
)

func TestKeyID_StableAndDistinct(t *testing.T) {
	a1, err := KeyID(keyA)
	if err != nil {
		t.Fatalf("KeyID(keyA): %v", err)
	}
	a2, _ := KeyID(keyA)
	if a1 != a2 {
		t.Errorf("KeyID not stable: %s vs %s", a1, a2)
	}
	b, _ := KeyID(keyB)
	if a1 == b {
		t.Errorf("KeyID collision between distinct keys: %s", a1)
	}
	if len(a1) != keyIDLen {
		t.Errorf("KeyID length = %d, want %d", len(a1), keyIDLen)
	}
}

func TestKeyID_RejectsBadKey(t *testing.T) {
	if _, err := KeyID("nothex"); err == nil {
		t.Error("expected error for non-hex key")
	}
	if _, err := KeyID("aabb"); err == nil {
		t.Error("expected error for short key")
	}
}

func TestKeyringEnvelopeRoundTrip(t *testing.T) {
	kr, err := NewKeyring(keyA)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	env, err := kr.Encrypt("super-secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Envelope must be key-id-tagged.
	id, _ := KeyID(keyA)
	if want := id + ":"; len(env) <= len(want) || env[:len(want)] != want {
		t.Errorf("envelope %q not tagged with %q", env, want)
	}
	got, err := kr.Decrypt(env)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "super-secret" {
		t.Errorf("round-trip = %q, want super-secret", got)
	}
}

func TestKeyringDecryptsLegacyUntagged(t *testing.T) {
	// A value produced by the old Encrypt (no key-id prefix) must still decrypt
	// under the primary key.
	legacy, err := Encrypt(keyA, "legacy-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	kr, _ := NewKeyring(keyA)
	got, err := kr.Decrypt(legacy)
	if err != nil {
		t.Fatalf("Decrypt legacy: %v", err)
	}
	if got != "legacy-value" {
		t.Errorf("legacy decrypt = %q", got)
	}
}

func TestKeyringMultiKeyDecrypt(t *testing.T) {
	// A keyring holding both keys decrypts envelopes tagged with either.
	krA, _ := NewKeyring(keyA)
	envA, _ := krA.Encrypt("from-A")

	krB, _ := NewKeyring(keyB)
	envB, _ := krB.Encrypt("from-B")

	both, _ := NewKeyring(keyB, keyA) // primary B, also knows A
	if got, err := both.Decrypt(envA); err != nil || got != "from-A" {
		t.Errorf("decrypt envA: got %q err %v", got, err)
	}
	if got, err := both.Decrypt(envB); err != nil || got != "from-B" {
		t.Errorf("decrypt envB: got %q err %v", got, err)
	}
}

func TestKeyringUnknownKeyID(t *testing.T) {
	krB, _ := NewKeyring(keyB)
	envB, _ := krB.Encrypt("x")

	krA, _ := NewKeyring(keyA) // doesn't know B
	if _, err := krA.Decrypt(envB); err == nil {
		t.Error("expected error decrypting an envelope tagged with an unknown key-id")
	}
}
