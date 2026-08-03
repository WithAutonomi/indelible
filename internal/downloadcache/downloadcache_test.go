package downloadcache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// writeTemp creates a fully-written temp file on the same filesystem as the
// store root, mirroring how the download/upload paths stage bytes.
func writeTemp(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "tmp-"+strings.ReplaceAll(t.Name(), "/", "_"))
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestPromoteGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects"))
	src := writeTemp(t, dir, "cached bytes")

	if _, err := s.Promote(testKey, src); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("promotion must move the source file, not copy it")
	}

	p, ok := s.Get(testKey)
	if !ok {
		t.Fatal("promoted entry missed")
	}
	// Two-level fanout: objects/ab/cd/<key>.
	want := filepath.Join(dir, "objects", testKey[0:2], testKey[2:4], testKey)
	if p != want {
		t.Fatalf("hit path = %q, want %q", p, want)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "cached bytes" {
		t.Fatalf("cached content = %q (err=%v), want %q", b, err, "cached bytes")
	}

	count, bytes := s.Stats()
	if count != 1 || bytes != int64(len("cached bytes")) {
		t.Fatalf("stats = (%d, %d), want (1, %d)", count, bytes, len("cached bytes"))
	}
}

func TestPromoteRejectsInvalidKeys(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects"))

	for _, key := range []string{
		"",
		"short",
		"../../../../etc/passwd",
		"0123456789ABCDEF0123456789ABCDEF", // uppercase
		testKey[:20] + "/" + testKey[21:],  // separator inside
		strings.Repeat("a", 200),           // too long
	} {
		src := writeTemp(t, dir, "x")
		if _, err := s.Promote(key, src); err == nil {
			t.Errorf("key %q accepted, want rejection", key)
		}
		if _, err := os.Stat(src); err != nil {
			t.Errorf("key %q: source removed on rejection", key)
		}
		os.Remove(src)
	}
}

func TestGetMissesUnknownKey(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "objects"))
	if _, ok := s.Get(testKey); ok {
		t.Fatal("unknown key reported a hit")
	}
}

func TestGetSelfHealsExternalDeletion(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects"))
	if _, err := s.Promote(testKey, writeTemp(t, dir, "bytes")); err != nil {
		t.Fatalf("promote: %v", err)
	}

	p, _ := s.Get(testKey)
	if err := os.Remove(p); err != nil {
		t.Fatalf("external remove: %v", err)
	}

	if _, ok := s.Get(testKey); ok {
		t.Fatal("hit on an externally deleted file")
	}
	if count, _ := s.Stats(); count != 0 {
		t.Fatal("stale index entry survived the self-heal")
	}
}

func TestGetSelfHealsSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects"))
	if _, err := s.Promote(testKey, writeTemp(t, dir, "full content")); err != nil {
		t.Fatalf("promote: %v", err)
	}

	p, _ := s.Get(testKey)
	if err := os.Truncate(p, 3); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	if _, ok := s.Get(testKey); ok {
		t.Fatal("hit on a size-mismatched file")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("corrupt file left on disk after self-heal")
	}
}

func TestDropRemovesEntryAndFile(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects"))
	if _, err := s.Promote(testKey, writeTemp(t, dir, "bytes")); err != nil {
		t.Fatalf("promote: %v", err)
	}
	p, _ := s.Get(testKey)

	s.Drop(testKey)
	if _, ok := s.Get(testKey); ok {
		t.Fatal("dropped key still hits")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("dropped file still on disk")
	}
	s.Drop(testKey) // absent key: no-op, no panic
}

func TestScanAdoptsPreviousRun(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "objects")
	prev := New(root)
	if _, err := prev.Promote(testKey, writeTemp(t, dir, "persisted")); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// A junk file inside the tree (crashed process leftover) and a valid key
	// at the wrong fanout position must both be cleaned up, not adopted.
	junk := filepath.Join(root, testKey[0:2], "leftover.tmp")
	if err := os.WriteFile(junk, []byte("junk"), 0600); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	misplacedKey := strings.Repeat("f", 64)
	misplaced := filepath.Join(root, "00", "00", misplacedKey)
	if err := os.MkdirAll(filepath.Dir(misplaced), 0700); err != nil {
		t.Fatalf("mkdir misplaced: %v", err)
	}
	if err := os.WriteFile(misplaced, []byte("misplaced"), 0600); err != nil {
		t.Fatalf("write misplaced: %v", err)
	}

	s := New(root)
	if _, ok := s.Get(testKey); ok {
		t.Fatal("hit before Scan — index must start empty")
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if _, ok := s.Get(testKey); !ok {
		t.Fatal("scan did not adopt the previous run's entry")
	}
	if _, ok := s.Get(misplacedKey); ok {
		t.Fatal("scan adopted a misplaced entry")
	}
	for _, p := range []string{junk, misplaced} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("scan left %s on disk", p)
		}
	}
	if count, bytes := s.Stats(); count != 1 || bytes != int64(len("persisted")) {
		t.Fatalf("stats after scan = (%d, %d), want (1, %d)", count, bytes, len("persisted"))
	}
}

func TestScanOnMissingRootIsEmptyCache(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "objects"))
	if err := s.Scan(context.Background()); err != nil {
		t.Fatalf("scan on missing root: %v", err)
	}
	if count, _ := s.Stats(); count != 0 {
		t.Fatal("phantom entries from a missing root")
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects"))

	keys := make([]string, 8)
	for i := range keys {
		keys[i] = strings.Repeat("0123456789abcdef"[i:i+1], 64)
	}

	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			src := filepath.Join(dir, "tmp-"+key[:8])
			if err := os.WriteFile(src, []byte(strings.Repeat("x", i+1)), 0600); err != nil {
				t.Errorf("write temp: %v", err)
				return
			}
			if _, err := s.Promote(key, src); err != nil {
				t.Errorf("promote %s: %v", key[:8], err)
				return
			}
			for j := 0; j < 50; j++ {
				if _, ok := s.Get(key); !ok {
					t.Errorf("miss on own key %s", key[:8])
					return
				}
			}
			s.Drop(key)
		}(i, key)
	}
	wg.Wait()

	if count, bytes := s.Stats(); count != 0 || bytes != 0 {
		t.Fatalf("stats after drain = (%d, %d), want (0, 0)", count, bytes)
	}
}
