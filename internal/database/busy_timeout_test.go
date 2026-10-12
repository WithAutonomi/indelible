package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

// TestBusyTimeoutOnEveryPooledConnection pins the fix for the SQLITE_BUSY
// concurrent-finalize failure (exposed when V2-1112 coalescing made three
// uploads complete in the same instant): busy_timeout is a connection-level
// pragma, so it must ride the DSN (applied by modernc.org/sqlite to every
// connection it opens), not a one-shot Exec that covers a single pooled
// connection.
func TestBusyTimeoutOnEveryPooledConnection(t *testing.T) {
	db, err := Open("sqlite://" + filepath.Join(t.TempDir(), "busy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Hold several distinct pool connections open at once and read the pragma
	// on each — with the old Exec-once approach only the first would be 5000.
	ctx := context.Background()
	const n = 4
	conns := make([]*sql.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < n; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		var timeout int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 {
			t.Fatalf("connection %d: busy_timeout = %d, want 5000", i, timeout)
		}
	}
}

// TestConcurrentWritersDoNotHitBusy is the behavioral half: many goroutines
// writing transactionally at the same instant must all succeed (the
// busy_timeout makes writers queue instead of failing "database is locked").
func TestConcurrentWritersDoNotHitBusy(t *testing.T) {
	db, err := Open("sqlite://" + filepath.Join(t.TempDir(), "writers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)`); err != nil {
		t.Fatal(err)
	}

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.Begin()
			if err != nil {
				errs <- err
				return
			}
			if _, err := tx.Exec(`INSERT INTO t (v) VALUES (?)`, "x"); err != nil {
				tx.Rollback()
				errs <- err
				return
			}
			errs <- tx.Commit()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent writer failed: %v", err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM t`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != writers {
		t.Fatalf("count = %d, want %d", count, writers)
	}
}
