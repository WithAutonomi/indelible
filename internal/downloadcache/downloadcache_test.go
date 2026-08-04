package downloadcache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// testBudget is a byte budget far above anything these tests promote, for
// tests that aren't about budget enforcement.
const testBudget = int64(1) << 40

// newReadyStore builds a Store over root and completes the boot scan, the
// same sequence production runs — promotion is refused until Scan finishes.
func newReadyStore(t *testing.T, root string) *Store {
	t.Helper()
	s := New(root)
	if err := s.Scan(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return s
}

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
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	src := writeTemp(t, dir, "cached bytes")

	if _, err := s.PromoteIfFits(testKey, src, testBudget); err != nil {
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
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("cached file mode = %v (err=%v), want 0600", fi.Mode().Perm(), err)
	}

	count, bytes := s.Stats()
	if count != 1 || bytes != int64(len("cached bytes")) {
		t.Fatalf("stats = (%d, %d), want (1, %d)", count, bytes, len("cached bytes"))
	}
}

func TestPromoteRejectsInvalidKeys(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))

	for _, key := range []string{
		"",
		"short",
		"../../../../etc/passwd",
		"0123456789ABCDEF0123456789ABCDEF", // uppercase
		testKey[:20] + "/" + testKey[21:],  // separator inside
		strings.Repeat("a", 200),           // too long
	} {
		src := writeTemp(t, dir, "x")
		if _, err := s.PromoteIfFits(key, src, testBudget); err == nil {
			t.Errorf("key %q accepted, want rejection", key)
		}
		if _, err := os.Stat(src); err != nil {
			t.Errorf("key %q: source removed on rejection", key)
		}
		os.Remove(src)
	}
}

// Promotion must be refused until the boot scan has made the index
// authoritative — before that the byte accounting undercounts whatever a
// previous run left on disk, and a budget admitted against it is fiction.
func TestPromoteRefusedBeforeScan(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "objects")) // deliberately no Scan
	src := writeTemp(t, dir, "too early")

	if _, err := s.PromoteIfFits(testKey, src, testBudget); !errors.Is(err, ErrNotReady) {
		t.Fatalf("promote before scan: err = %v, want ErrNotReady", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must be left in place on refusal")
	}
}

// A previous run's on-disk bytes count against the budget as soon as the
// scan adopts them: a new promotion that would exceed the budget on top of
// the pre-existing usage is refused.
func TestPromoteAccountsPreexistingFiles(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "objects")
	prev := newReadyStore(t, root)
	if _, err := prev.PromoteIfFits(testKey, writeTemp(t, dir, strings.Repeat("x", 20)), testBudget); err != nil {
		t.Fatalf("seed promote: %v", err)
	}

	s := newReadyStore(t, root) // fresh process: adopts the 20 bytes
	if _, bytes := s.Stats(); bytes != 20 {
		t.Fatalf("adopted bytes = %d, want 20", bytes)
	}
	otherKey := strings.Repeat("f", 64)
	src := writeTemp(t, dir, strings.Repeat("y", 15))
	// 20 on disk + 15 new > 30 budget.
	if _, err := s.PromoteIfFits(otherKey, src, 30); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("promote over pre-existing usage: err = %v, want ErrOverBudget", err)
	}
	// A budget that fits admits it.
	if _, err := s.PromoteIfFits(otherKey, src, 40); err != nil {
		t.Fatalf("promote within budget: %v", err)
	}
}

// Regression for the concurrency blocker on PR #146: the budget check and
// the promotion must be one atomic step. N concurrent promotions of distinct
// keys, each fitting alone but not together, must never leave the indexed
// total above budget — only the fitting subset lands.
func TestConcurrentPromotionsRespectBudget(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	const budget = int64(25) // fits two 10-byte objects, never three

	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := 0; i < 8; i++ {
		key := strings.Repeat("0123456789abcdef"[i:i+1], 64)
		src := filepath.Join(dir, "tmp-"+key[:8])
		if err := os.WriteFile(src, []byte(strings.Repeat("x", 10)), 0600); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		wg.Add(1)
		go func(i int, key, src string) {
			defer wg.Done()
			_, results[i] = s.PromoteIfFits(key, src, budget)
		}(i, key, src)
	}
	wg.Wait()

	promoted := 0
	for i, err := range results {
		switch {
		case err == nil:
			promoted++
		case errors.Is(err, ErrOverBudget):
		default:
			t.Errorf("promotion %d: unexpected error %v", i, err)
		}
	}
	if promoted != 2 {
		t.Errorf("promoted = %d, want exactly 2 (budget 25 / size 10)", promoted)
	}
	if _, bytes := s.Stats(); bytes > budget {
		t.Fatalf("indexed bytes = %d, exceeds budget %d", bytes, budget)
	}
	// The index and the disk agree.
	files := 0
	_ = filepath.WalkDir(filepath.Join(dir, "objects"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != promoted {
		t.Fatalf("files on disk = %d, promoted = %d", files, promoted)
	}
}

func TestGetMissesUnknownKey(t *testing.T) {
	s := newReadyStore(t, filepath.Join(t.TempDir(), "objects"))
	if _, ok := s.Get(testKey); ok {
		t.Fatal("unknown key reported a hit")
	}
}

func TestGetSelfHealsExternalDeletion(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "bytes"), testBudget); err != nil {
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
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "full content"), testBudget); err != nil {
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

// An entry swapped for a symlink on disk must never be followed and served.
func TestGetRejectsSymlinkSwap(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "real"), testBudget); err != nil {
		t.Fatalf("promote: %v", err)
	}
	p, _ := s.Get(testKey)

	target := writeTemp(t, dir, "real") // same size as the entry
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, ok := s.Get(testKey); ok {
		t.Fatal("hit on a symlinked entry")
	}
}

func TestDropRemovesEntryAndFile(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "bytes"), testBudget); err != nil {
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
	prev := newReadyStore(t, root)
	if _, err := prev.PromoteIfFits(testKey, writeTemp(t, dir, "persisted"), testBudget); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// Junk (crashed-process leftover), a valid key at the wrong fanout
	// position, and a symlink must all be cleaned up, not adopted.
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
	linkKey := strings.Repeat("e", 64)
	link := filepath.Join(root, "ee", "ee", linkKey)
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatalf("mkdir link: %v", err)
	}
	if err := os.Symlink(junk, link); err != nil {
		t.Fatalf("symlink: %v", err)
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
	for name, k := range map[string]string{"misplaced": misplacedKey, "symlinked": linkKey} {
		if _, ok := s.Get(k); ok {
			t.Fatalf("scan adopted a %s entry", name)
		}
	}
	for _, p := range []string{junk, misplaced, link} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("scan left %s on disk", p)
		}
	}
	if count, bytes := s.Stats(); count != 1 || bytes != int64(len("persisted")) {
		t.Fatalf("stats after scan = (%d, %d), want (1, %d)", count, bytes, len("persisted"))
	}
}

func TestScanOnMissingRootIsEmptyCache(t *testing.T) {
	s := newReadyStore(t, filepath.Join(t.TempDir(), "objects"))
	if count, _ := s.Stats(); count != 0 {
		t.Fatal("phantom entries from a missing root")
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))

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
			if _, err := s.PromoteIfFits(key, src, testBudget); err != nil {
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

// setAccess backdates an entry's access clock directly — deterministic LRU
// ordering without sleeping through wall-clock granularity.
func setAccess(t *testing.T, s *Store, key string, at time.Time) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		t.Fatalf("setAccess: no entry for %s", key[:8])
	}
	e.lastAccess = at
}

func TestOldestOrdersByAccessAndBoundsResult(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	keys := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	now := time.Now()
	for i, k := range keys {
		if _, err := s.PromoteIfFits(k, writeTemp(t, dir, "x"), testBudget); err != nil {
			t.Fatalf("promote %d: %v", i, err)
		}
	}
	// Access order (oldest first) deliberately differs from promotion order.
	setAccess(t, s, keys[1], now.Add(-3*time.Hour))
	setAccess(t, s, keys[2], now.Add(-2*time.Hour))
	setAccess(t, s, keys[0], now.Add(-1*time.Hour))

	victims := s.Oldest(10)
	if len(victims) != 3 {
		t.Fatalf("Oldest returned %d victims, want 3", len(victims))
	}
	for i, want := range []string{keys[1], keys[2], keys[0]} {
		if victims[i].Key != want {
			t.Fatalf("victim[%d] = %s, want %s", i, victims[i].Key[:8], want[:8])
		}
	}
	if got := s.Oldest(2); len(got) != 2 || got[0].Key != keys[1] {
		t.Fatalf("Oldest(2) = %d victims starting %s, want 2 starting %s", len(got), got[0].Key[:8], keys[1][:8])
	}
}

func TestDropGenEvictsOnlyTheObservedPromotion(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "old bytes"), testBudget); err != nil {
		t.Fatalf("promote: %v", err)
	}
	victims := s.Oldest(1)
	if len(victims) != 1 {
		t.Fatal("expected one victim")
	}

	// The sweeper's snapshot goes stale: the key is re-promoted before the
	// drop lands. The stale-gen drop must be a no-op — the fresh file and its
	// accounting survive.
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "fresh bytes!"), testBudget); err != nil {
		t.Fatalf("re-promote: %v", err)
	}
	if s.DropGen(victims[0].Key, victims[0].Gen) {
		t.Fatal("stale-gen DropGen acted on a fresh promotion")
	}
	p, ok := s.Get(testKey)
	if !ok {
		t.Fatal("fresh promotion lost to a stale eviction")
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "fresh bytes!" {
		t.Fatalf("cached content = %q, %v; want fresh bytes", b, err)
	}
	if count, bytes := s.Stats(); count != 1 || bytes != int64(len("fresh bytes!")) {
		t.Fatalf("stats = (%d, %d), want (1, %d)", count, bytes, len("fresh bytes!"))
	}

	// A current-gen drop acts: entry, file, and accounting all go.
	current := s.Oldest(1)
	if !s.DropGen(current[0].Key, current[0].Gen) {
		t.Fatal("current-gen DropGen refused to act")
	}
	if _, ok := s.Get(testKey); ok {
		t.Fatal("evicted entry still served")
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("evicted file still on disk: %v", err)
	}
	if count, bytes := s.Stats(); count != 0 || bytes != 0 {
		t.Fatalf("stats after eviction = (%d, %d), want (0, 0)", count, bytes)
	}
}

func TestOpenKeepsServingAcrossEviction(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	const content = "still readable after unlink"
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, content), testBudget); err != nil {
		t.Fatalf("promote: %v", err)
	}

	f, ok := s.Open(testKey)
	if !ok {
		t.Fatal("open missed on a promoted entry")
	}
	defer f.Close()

	// Evict while the descriptor is open — the serve in flight must not care.
	v := s.Oldest(1)[0]
	if !s.DropGen(v.Key, v.Gen) {
		t.Fatal("eviction refused")
	}
	b, err := io.ReadAll(f)
	if err != nil || string(b) != content {
		t.Fatalf("read across eviction = %q, %v; want full content", b, err)
	}
	if _, ok := s.Get(testKey); ok {
		t.Fatal("evicted entry still indexed")
	}
}

func TestOpenSelfHealsOnExternalDeletion(t *testing.T) {
	dir := t.TempDir()
	s := newReadyStore(t, filepath.Join(dir, "objects"))
	if _, err := s.PromoteIfFits(testKey, writeTemp(t, dir, "bytes"), testBudget); err != nil {
		t.Fatalf("promote: %v", err)
	}
	p, _ := s.Get(testKey)
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := s.Open(testKey); ok {
		t.Fatal("open served a deleted file")
	}
	if count, _ := s.Stats(); count != 0 {
		t.Fatal("self-heal did not drop the entry")
	}
}

func TestKeyForIdentifier(t *testing.T) {
	k := KeyForIdentifier("some-datamap-address")
	if len(k) != 64 || !keyValid(k) {
		t.Fatalf("key %q is not 64-char lowercase hex", k)
	}
	if KeyForIdentifier("some-datamap-address") != k {
		t.Fatal("derivation must be stable")
	}
	if KeyForIdentifier("other-address") == k {
		t.Fatal("distinct identifiers must yield distinct keys")
	}
	if KeyForIdentifier("") != "" {
		t.Fatal("empty identifier must yield empty key")
	}
}
