// Package downloadcache is a content-addressed, per-instance cache of
// immutable download bytes (V2-820/V2-821). Both transfer paths already
// materialise the full file on local disk and then delete it; this store lets
// them promote that file by rename instead, so repeat reads of small, hot,
// immutable objects are served from disk without touching antd or the network.
//
// The store is mechanism only. Eligibility policy — per-object size ceiling,
// public-only privacy default, min-uses admission, disk budget — belongs to
// callers and the (V2-823) sweeper; the store just promotes, serves, and
// forgets files. It is share-nothing by design: every instance owns its cache
// under its own DataDir, the index lives in memory (no DB writes — reader
// fleet discipline, V2-514), and entries are expendable — every one is
// reconstructible from the network, so eviction, wipe, or corruption have
// zero data-loss consequence.
//
// Keys are the upload's content identity — the unquoted downloadETag hex
// (sha256 of the DataMap/network address, V2-516) — so cache lookups, hit
// checks, and 304s share one identity. The key is a digest of the content
// *identifier*, not of the plaintext bytes, so it cannot be recomputed from a
// file on disk; the integrity guard is the size recorded at promotion,
// checked on every hit and on boot scan (mismatch ⇒ drop, caller refetches).
package downloadcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ErrNotReady is returned by PromoteIfFits before Scan has made the index
// authoritative: until every file a previous run left on disk is adopted,
// the byte accounting undercounts and a budget check would be meaningless.
var ErrNotReady = errors.New("downloadcache: index not ready (scan incomplete)")

// ErrOverBudget is returned by PromoteIfFits when admitting the object would
// push the indexed total over the caller's byte budget.
var ErrOverBudget = errors.New("downloadcache: promotion exceeds byte budget")

// KeyForIdentifier returns the cache key for an upload's content identifier
// (local DataMap or published network address): the hex sha256 of the
// domain-separated identifier — the same digest the download ETag quotes.
// One derivation shared by the serve path and upload-side seeding (V2-822),
// so both sides always agree on an object's identity, and neither ever
// exposes the raw identifier (the DataMap is the retrieval capability).
// Returns "" for an empty identifier.
func KeyForIdentifier(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("indelible-download-v1:" + id))
	return hex.EncodeToString(sum[:])
}

// keyValid reports whether key is safe as a cache filename: lowercase hex,
// long enough to be a real digest, so a hostile key can never traverse paths.
func keyValid(key string) bool {
	if len(key) < 16 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

type entry struct {
	size       int64
	lastAccess time.Time // in-memory LRU/inactivity clock (V2-823) — never filesystem atime
	// gen identifies this particular promotion of the key. The sweeper holds
	// (key, gen) pairs from an Oldest snapshot; DropGen refuses to act when
	// the generations differ, so eviction planned against one file can never
	// unlink the fresher file a concurrent re-promotion put in its place.
	gen uint64
}

// Store is the in-memory index over a directory of promoted files. The
// directory is the durable state; the index is rebuilt from it by Scan at
// boot. Safe for concurrent use.
type Store struct {
	root string

	mu      sync.Mutex
	entries map[string]*entry
	bytes   int64  // running total of indexed entry sizes
	ready   bool   // set once Scan has adopted the previous run's files
	genSeq  uint64 // source of entry generations; incremented per promotion/adoption

	// lastUnlinkWarn rate-limits the failed-unlink warning (see unlinkLocked);
	// guarded by mu like everything else here.
	lastUnlinkWarn time.Time

	metrics Metrics // V2-825 counters; see metrics.go
}

// New returns a Store rooted at dir (conventionally DataDir/cache/objects).
// The index starts empty — run Scan to adopt files from a previous run;
// lookups before/during the scan simply miss, which is safe (callers
// refetch), and promotions are refused (ErrNotReady) until Scan completes.
func New(dir string) *Store {
	return &Store{root: dir, entries: make(map[string]*entry)}
}

// path returns the file location for key: a two-level fanout
// (objects/ab/cd/<key>) so directories stay small at large object counts.
func (s *Store) path(key string) string {
	return filepath.Join(s.root, key[0:2], key[2:4], key)
}

// Get returns the on-disk path for key if the entry exists and still matches
// its recorded size. A hit refreshes the entry's access clock. If the file
// vanished or changed size out from under the index (external deletion,
// truncation, corruption), the entry is dropped and Get reports a miss — the
// cache is expendable, the caller just refetches.
//
// Serve paths should prefer Open: a path returned here can be unlinked by the
// (V2-823) sweeper before the caller opens it, turning a hit into a 404,
// whereas an open descriptor keeps serving the unlinked bytes.
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		s.mu.Unlock()
		return "", false
	}
	size, gen := e.size, e.gen
	s.mu.Unlock()

	p := s.path(key)
	// Lstat outside the lock: cheap, but no reason to serialize other lookups
	// behind disk latency. Lstat (not Stat) plus the regular-file check means
	// an entry swapped for a symlink or device node on disk is dropped, never
	// followed and served. Self-healing is gen-checked: if the mismatch was a
	// concurrent eviction already replaced by a fresh promotion, the fresh
	// entry survives and this lookup just misses.
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		if s.DropGen(key, gen) {
			s.metrics.SelfHealDrops.Add(1)
		}
		return "", false
	}

	s.mu.Lock()
	if e, ok := s.entries[key]; ok {
		e.lastAccess = time.Now()
	}
	s.mu.Unlock()
	return p, true
}

// Open returns an open descriptor for key's cached bytes, or false on a miss.
// This is the serve-path lookup: once the descriptor is returned, concurrent
// eviction (the V2-823 sweeper unlinks files) cannot fail the serve — on
// POSIX the unlinked inode keeps streaming until the descriptor closes. The
// caller owns the returned file and must Close it.
//
// The entry is revalidated before returning: Lstat rejects a symlink or
// non-regular swap without following it, and after opening, the descriptor's
// own fstat must still report a regular file of the recorded size — so a file
// swapped between the check and the open is caught, and identity mismatches
// self-heal exactly like Get (drop, report a miss, caller refetches). A hit
// refreshes the entry's access clock.
func (s *Store) Open(key string) (*os.File, bool) {
	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	size, gen := e.size, e.gen
	s.mu.Unlock()

	p := s.path(key)
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		if s.DropGen(key, gen) {
			s.metrics.SelfHealDrops.Add(1)
		}
		return nil, false
	}
	f, err := os.Open(p)
	if err != nil {
		// Vanished between the Lstat and the open (eviction races are that
		// narrow). Gen-checked self-heal: never disturbs a fresh re-promotion.
		if s.DropGen(key, gen) {
			s.metrics.SelfHealDrops.Add(1)
		}
		return nil, false
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		_ = f.Close()
		if s.DropGen(key, gen) {
			s.metrics.SelfHealDrops.Add(1)
		}
		return nil, false
	}

	s.mu.Lock()
	if e, ok := s.entries[key]; ok {
		e.lastAccess = time.Now()
	}
	s.mu.Unlock()
	// Every successful Open is a serve from local disk. Bytes count the whole
	// object per hit — Range responses send less on the wire, but the number
	// exists to compare against BytesFetch (what antd would have re-fetched),
	// and a fetch would also have been the whole object.
	s.metrics.Hits.Add(1)
	s.metrics.BytesServed.Add(size)
	return f, true
}

// PromoteIfFits moves srcPath (a fully-written temp file on the same
// filesystem) into the cache under key iff the indexed total would stay
// within budget, and returns the entry's path. The budget check, the rename,
// and the byte accounting are one critical section, so two concurrent
// promotions of distinct keys can never both spend the same remaining bytes
// — holding the mutex across a same-filesystem rename is microseconds, and
// promotion only happens on the (network-bound) miss path. The rename is
// atomic, so a partially-written file is never visible; on success srcPath no
// longer exists. Promoting a key that is already cached atomically replaces
// it (same content — keys are content-addressed) and only the size delta
// counts against the budget. On error the source file is left in place for
// the caller's own cleanup.
//
// Promotion is refused with ErrNotReady until Scan has adopted a previous
// run's files: before that the accounting undercounts what is already on
// disk, and a budget admitted against it would be fiction.
func (s *Store) PromoteIfFits(key, srcPath string, budget int64) (string, error) {
	if !keyValid(key) {
		return "", fs.ErrInvalid
	}
	fi, err := os.Lstat(srcPath)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fs.ErrInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return "", ErrNotReady
	}
	delta := fi.Size()
	if old, ok := s.entries[key]; ok {
		delta -= old.size
	}
	if s.bytes+delta > budget {
		return "", ErrOverBudget
	}
	dst := s.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return "", err
	}
	if err := os.Rename(srcPath, dst); err != nil {
		return "", err
	}
	// The bytes came from antd with whatever mode the daemon wrote; pin the
	// cached copy to owner-only for shared-volume deployments.
	_ = os.Chmod(dst, 0600)
	s.genSeq++
	s.entries[key] = &entry{size: fi.Size(), lastAccess: time.Now(), gen: s.genSeq}
	s.bytes += delta
	return dst, nil
}

// Drop removes key's bytes from this instance — the purge primitive for the
// V2-824 delete invariant, unconditional where DropGen is generation-checked.
// A nil return guarantees no file for key remains on this instance's disk:
// the unlink runs even when the key is not indexed (a failed boot scan
// leaves files present but unindexed, and the delete path must still be able
// to purge them), and an already-absent file counts as success. A non-nil
// return means the file is still on disk — the entry (if indexed) stays
// accounted, and the caller must not report the content as purged.
//
// Safe under a concurrent serve: on POSIX an open descriptor keeps streaming
// the unlinked bytes, and later lookups miss and refetch.
//
// The unlink happens inside the same critical section as the index update —
// the same lock promotion's rename runs under — so a Drop that decided to
// remove one promotion of the key can never end up unlinking a fresher file
// a concurrent re-promotion renamed into place after the decision. And the
// unlink comes FIRST: if it fails, the entry stays indexed (bytes still on
// disk must stay accounted) and a later Drop retries.
func (s *Store) Drop(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed, err := s.unlinkLocked(key)
	if err != nil {
		return err
	}
	if e, ok := s.entries[key]; ok {
		s.bytes -= e.size
		delete(s.entries, key)
		removed = true
	}
	if removed {
		s.metrics.Purged.Add(1)
	}
	return nil
}

// DropGen is Drop conditioned on the entry still being the exact promotion
// the caller observed: it removes key only if the current entry's generation
// matches gen, reporting whether it acted. This is the eviction primitive for
// the V2-823 sweeper (and lookups' self-healing): a (key, gen) pair taken
// from an Oldest snapshot stays safe to act on no matter how stale it gets,
// because a re-promotion in the meantime bumps the generation and the drop
// degrades to a no-op instead of unlinking the fresh file.
//
// True means the entry is gone AND its file is absent from disk — the unlink
// runs before the index/accounting mutation, so a failed unlink leaves the
// entry indexed (returning false) rather than stranding unaccounted bytes the
// sweeper would never revisit.
func (s *Store) DropGen(key string, gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok || e.gen != gen {
		return false
	}
	if _, err := s.unlinkLocked(key); err != nil {
		return false
	}
	s.bytes -= e.size
	delete(s.entries, key)
	return true
}

// unlinkLocked removes key's file, reporting whether a file was actually
// removed and whether the file is now absent (nil error). An already-missing
// file counts as success (the goal state holds); any other failure is logged
// — rate-limited to once a minute per store, since a persistently unwritable
// cache dir would otherwise warn per victim per sweep tick — and returned so
// callers keep the entry indexed: eviction and purge must never report bytes
// as freed while they still occupy the disk. Caller holds s.mu.
func (s *Store) unlinkLocked(key string) (removed bool, err error) {
	if !keyValid(key) {
		// Unreachable for indexed entries (Scan and PromoteIfFits enforce
		// keyValid), kept as a path-traversal backstop: no file to unlink.
		return false, nil
	}
	err = os.Remove(s.path(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	if time.Since(s.lastUnlinkWarn) >= time.Minute {
		s.lastUnlinkWarn = time.Now()
		slog.Warn("download cache unlink failed; keeping entry indexed for retry",
			"key", key, "error", err)
	}
	return false, err
}

// Victim is one entry of an Oldest snapshot: everything the sweeper needs to
// pick and safely evict a candidate (DropGen with the recorded generation).
type Victim struct {
	Key        string
	Gen        uint64
	Size       int64
	LastAccess time.Time
}

// Oldest returns up to max entries ordered least-recently-accessed first —
// the sweeper's LRU candidate list. It is a snapshot: entries may be
// accessed, replaced, or removed after it is taken, which is why eviction
// goes through DropGen rather than trusting the snapshot. (An entry accessed
// after the snapshot keeps its generation, so it can still be evicted a beat
// after a hit — the sweeper's batches run within milliseconds of the
// snapshot, and a wrongly evicted entry just refetches, so approximate LRU
// is accepted here like in every surveyed production cache.)
func (s *Store) Oldest(max int) []Victim {
	s.mu.Lock()
	victims := make([]Victim, 0, len(s.entries))
	for k, e := range s.entries {
		victims = append(victims, Victim{Key: k, Gen: e.gen, Size: e.size, LastAccess: e.lastAccess})
	}
	s.mu.Unlock()

	sort.Slice(victims, func(i, j int) bool {
		return victims[i].LastAccess.Before(victims[j].LastAccess)
	})
	if max >= 0 && len(victims) > max {
		victims = victims[:max]
	}
	return victims
}

// Scan walks the cache directory and adopts every valid file into the index,
// so a restart inherits the previous run's cache. Files that are not valid
// cache entries (stray names, wrong fanout position, symlinks and other
// non-regular files) are deleted — nothing else may live under the cache
// root, and leftovers from a crashed process are garbage by definition.
// Lookups during the scan safely miss against the partial index; promotion
// stays refused (ErrNotReady) until the scan completes successfully, since a
// budget admitted against an undercounting index would be fiction.
//
// Access clocks start at the scan time — the previous run's recency is not
// persisted, which only ages eviction decisions by one restart.
func (s *Store) Scan(ctx context.Context) error {
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // no cache directory yet — empty cache
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			_ = os.Remove(p) // removes a symlink itself, never its target
			return nil
		}
		key := d.Name()
		if !keyValid(key) || s.path(key) != p {
			_ = os.Remove(p)
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil // vanished mid-scan; a later lookup would self-heal anyway
		}
		s.mu.Lock()
		if _, exists := s.entries[key]; !exists {
			s.genSeq++
			s.entries[key] = &entry{size: fi.Size(), lastAccess: time.Now(), gen: s.genSeq}
			s.bytes += fi.Size()
		}
		s.mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return nil
}

// Stats reports the entry count and total bytes indexed — the inputs for the
// V2-823 budget sweeper and V2-825 observability.
func (s *Store) Stats() (count int, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries), s.bytes
}
