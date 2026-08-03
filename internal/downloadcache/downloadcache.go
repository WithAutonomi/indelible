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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
}

// Store is the in-memory index over a directory of promoted files. The
// directory is the durable state; the index is rebuilt from it by Scan at
// boot. Safe for concurrent use.
type Store struct {
	root string

	mu      sync.Mutex
	entries map[string]*entry
	bytes   int64 // running total of indexed entry sizes
	ready   bool  // set once Scan has adopted the previous run's files
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
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		s.mu.Unlock()
		return "", false
	}
	size := e.size
	s.mu.Unlock()

	p := s.path(key)
	// Lstat outside the lock: cheap, but no reason to serialize other lookups
	// behind disk latency. Lstat (not Stat) plus the regular-file check means
	// an entry swapped for a symlink or device node on disk is dropped, never
	// followed and served.
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		s.Drop(key)
		return "", false
	}

	s.mu.Lock()
	if e, ok := s.entries[key]; ok {
		e.lastAccess = time.Now()
	}
	s.mu.Unlock()
	return p, true
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
	s.entries[key] = &entry{size: fi.Size(), lastAccess: time.Now()}
	s.bytes += delta
	return dst, nil
}

// Drop forgets key and unlinks its file. Delete-only, and safe under a
// concurrent serve: on POSIX an open descriptor keeps streaming the unlinked
// bytes, and later lookups miss and refetch. Dropping an absent key is a
// no-op — Drop is how eviction (V2-823) and the delete/shred purge (V2-824)
// will remove entries, and both may race lookups' self-healing.
func (s *Store) Drop(key string) {
	s.mu.Lock()
	e, ok := s.entries[key]
	if ok {
		s.bytes -= e.size
	}
	delete(s.entries, key)
	s.mu.Unlock()
	if ok && keyValid(key) {
		_ = os.Remove(s.path(key))
	}
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
			s.entries[key] = &entry{size: fi.Size(), lastAccess: time.Now()}
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
