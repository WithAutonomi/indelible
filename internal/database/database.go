package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

var memDBCounter atomic.Int64

// Open connects to the database specified by the URL.
// Supports sqlite:// and postgres:// schemes. The returned *DB transparently
// rewrites `?` placeholders to `$N` when the driver is Postgres — service code
// should write queries with `?` regardless of dialect.
func Open(dbURL string) (*DB, error) {
	driver, dsn, err := parseURL(dbURL)
	if err != nil {
		return nil, err
	}

	// For on-disk SQLite, MkdirAll the parent so a bare-binary first-run on a
	// fresh non-root box doesn't crash with the misleading
	// "out of memory (14)" wording that modernc.org/sqlite uses for
	// SQLITE_CANTOPEN. V2-302. No-op for :memory:, the file: indirection
	// from the memdb fallback, or shared-cache strings.
	if driver == "sqlite" {
		if path := sqliteFilePath(dsn); path != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, fmt.Errorf("creating SQLite data directory %q: %w", filepath.Dir(path), err)
			}
		}
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite pragmas for performance. (foreign_keys is set per-connection via the
	// DSN in parseURL — see the note there; a one-shot Exec would only cover one
	// pooled connection.)
	if driver == "sqlite" {
		if _, err := db.Exec(`
			PRAGMA journal_mode=WAL;
			PRAGMA busy_timeout=5000;
			PRAGMA synchronous=NORMAL;
		`); err != nil {
			db.Close()
			return nil, sqliteOpenError(err, dsn)
		}
	}

	// Connection pool tuning for Postgres
	if driver == "postgres" {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		if driver == "sqlite" {
			return nil, sqliteOpenError(err, dsn)
		}
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &DB{DB: db, driver: driver}, nil
}

// sqliteFilePath extracts the on-disk file path from a SQLite DSN, or returns
// "" for in-memory / shared-cache / unparseable forms where there's no parent
// directory to create.
func sqliteFilePath(dsn string) string {
	// Strip optional "file:" prefix and any query string suffix
	// (?mode=memory&cache=shared etc.). Bail on in-memory shapes.
	s := strings.TrimPrefix(dsn, "file:")
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	if s == "" || s == ":memory:" || strings.HasPrefix(s, "memdb") {
		return ""
	}
	return s
}

// sqliteOpenError wraps a SQLITE_CANTOPEN error with a hint about
// INDELIBLE_DATA_DIR / INDELIBLE_DB_URL. Other errors pass through unwrapped.
// modernc.org/sqlite reports CANTOPEN as the string "out of memory (14)" which
// is famously misleading; this surface gives operators something actionable.
func sqliteOpenError(err error, dsn string) error {
	if err == nil {
		return nil
	}
	// The driver returns a plain error type without an exported code; match
	// on the wording. "(14)" is SQLITE_CANTOPEN.
	if !strings.Contains(err.Error(), "(14)") {
		return fmt.Errorf("setting SQLite pragmas: %w", err)
	}
	path := sqliteFilePath(dsn)
	dir := ""
	if path != "" {
		dir = filepath.Dir(path)
	}
	hint := "set INDELIBLE_DB_URL to point at a writable path"
	if dir != "" {
		hint = fmt.Sprintf("data directory %q does not exist or is not writable — create it or override with INDELIBLE_DB_URL / INDELIBLE_DATA_DIR", dir)
	}
	return errors.New("cannot open SQLite database: " + hint + " (driver said: " + err.Error() + ")")
}

// parseURL converts a db_url into a driver name and DSN.
func parseURL(dbURL string) (driver, dsn string, err error) {
	switch {
	case strings.HasPrefix(dbURL, "sqlite://"):
		dsn := strings.TrimPrefix(dbURL, "sqlite://")
		// SQLite :memory: databases are per-connection by default. Use shared
		// cache with a unique name so all connections from the same Open() call
		// see the same database, while different Open() calls (e.g. separate
		// tests) each get their own isolated database.
		if dsn == ":memory:" {
			id := memDBCounter.Add(1)
			dsn = fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", id)
		}
		// foreign_keys is a *connection-level* pragma in SQLite — setting it once
		// after Open only covers a single pooled connection, leaving ON DELETE
		// CASCADE / FK enforcement unreliable on the rest. modernc.org/sqlite
		// applies _pragma params on every connection it opens, so set it here.
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "_pragma=foreign_keys(1)"
		return "sqlite", dsn, nil
	case strings.HasPrefix(dbURL, "postgres://"), strings.HasPrefix(dbURL, "postgresql://"):
		return "postgres", dbURL, nil
	default:
		return "", "", fmt.Errorf("unsupported database URL scheme: %s (use sqlite:// or postgres://)", dbURL)
	}
}
