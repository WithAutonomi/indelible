// Package dbtest provides a per-test database helper that dispatches
// between SQLite and Postgres based on the INDELIBLE_TEST_DB_URL env var.
//
// SQLite (default, env empty): opens an in-memory database. The shared-cache
// trick in database.Open keeps each *sql.DB isolated from peers.
//
// Postgres (env points at postgres://): on first call within the test
// process, seeds an `indelible_template` database with all migrations
// applied. Subsequent calls CREATE DATABASE test_<pid>_<counter> TEMPLATE
// indelible_template, which clones at the filesystem level in ~10–50ms.
// Each test gets bulletproof isolation at near schema-per-test speed.
//
// Tests should write helpers like:
//
//	func setupTestDB(t *testing.T) *database.DB {
//	    return dbtest.OpenDB(t)
//	}
package dbtest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/lib/pq"

	"github.com/WithAutonomi/indelible/internal/database"
)

// OpenDB returns a per-test *database.DB with all migrations applied.
// Driver is chosen by INDELIBLE_TEST_DB_URL (defaults to sqlite://:memory:).
// The database is dropped via t.Cleanup when the test finishes.
func OpenDB(t testing.TB) *database.DB {
	return open(t, true)
}

// OpenEmptyDB returns a per-test *database.DB without migrations applied.
// Intended for tests that exercise the migration machinery itself.
func OpenEmptyDB(t testing.TB) *database.DB {
	return open(t, false)
}

func open(t testing.TB, migrate bool) *database.DB {
	t.Helper()
	dbURL := os.Getenv("INDELIBLE_TEST_DB_URL")
	if dbURL == "" {
		dbURL = "sqlite://:memory:"
	}
	switch {
	case strings.HasPrefix(dbURL, "sqlite://"):
		return openSqlite(t, dbURL, migrate)
	case strings.HasPrefix(dbURL, "postgres://"), strings.HasPrefix(dbURL, "postgresql://"):
		return openPostgres(t, dbURL, migrate)
	default:
		t.Fatalf("INDELIBLE_TEST_DB_URL must be sqlite:// or postgres://, got %q", dbURL)
		return nil
	}
}

func openSqlite(t testing.TB, dbURL string, migrate bool) *database.DB {
	db, err := database.Open(dbURL)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if migrate {
		if err := database.Migrate(db, "sqlite"); err != nil {
			t.Fatalf("migrate sqlite: %v", err)
		}
	}
	return db
}

const templateDBName = "indelible_template"

var (
	templateOnce sync.Once
	templateErr  error
	pgCounter    atomic.Int64
)

func openPostgres(t testing.TB, adminURL string, migrate bool) *database.DB {
	if migrate {
		templateOnce.Do(func() { templateErr = seedTemplate(adminURL) })
		if templateErr != nil {
			t.Fatalf("seed template: %v", templateErr)
		}
	}

	dbName := fmt.Sprintf("test_%d_%d", os.Getpid(), pgCounter.Add(1))
	if err := createDB(adminURL, dbName, migrate); err != nil {
		t.Fatalf("create per-test db %s: %v", dbName, err)
	}

	perTestURL := urlWithDB(adminURL, dbName)
	db, err := database.Open(perTestURL)
	if err != nil {
		_ = dropDB(adminURL, dbName)
		t.Fatalf("open per-test db %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		db.Close()
		_ = dropDB(adminURL, dbName)
	})
	return db
}

// templateLockKey is an arbitrary 64-bit key for the cross-process
// pg_advisory_lock that serialises template seeding. Go's test runner
// invokes each package in its own process, so `sync.Once` alone is not
// enough — without the lock, two processes can race to CREATE DATABASE
// indelible_template and one fails with a unique-constraint violation.
const templateLockKey int64 = 0xC0FFEE_DB_5EED

func seedTemplate(adminURL string) error {
	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		return fmt.Errorf("open admin: %w", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(`SELECT pg_advisory_lock($1)`, templateLockKey); err != nil {
		return fmt.Errorf("acquire seed lock: %w", err)
	}
	defer func() { _, _ = admin.Exec(`SELECT pg_advisory_unlock($1)`, templateLockKey) }()

	var exists bool
	if err := admin.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, templateDBName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check template existence: %w", err)
	}
	if exists {
		// Another process already seeded it. Trust the existing template;
		// migrations only diverge between code revisions, and each CI run
		// gets a fresh container anyway. Local devs running stale templates
		// can manually `DROP DATABASE indelible_template`.
		return nil
	}

	if _, err := admin.Exec(`CREATE DATABASE "` + templateDBName + `"`); err != nil {
		return fmt.Errorf("create template: %w", err)
	}

	tmplURL := urlWithDB(adminURL, templateDBName)
	tdb, err := database.Open(tmplURL)
	if err != nil {
		return fmt.Errorf("open template: %w", err)
	}
	defer tdb.Close()
	if err := database.Migrate(tdb, "postgres"); err != nil {
		return fmt.Errorf("migrate template: %w", err)
	}
	return nil
}

func createDB(adminURL, name string, fromTemplate bool) error {
	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		return err
	}
	defer admin.Close()
	stmt := `CREATE DATABASE "` + name + `"`
	if fromTemplate {
		stmt += ` TEMPLATE "` + templateDBName + `"`
	}
	_, err = admin.Exec(stmt)
	return err
}

func dropDB(adminURL, name string) error {
	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		return err
	}
	defer admin.Close()
	_, err = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`)
	return err
}

func urlWithDB(base, dbName string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = "/" + dbName
	return u.String()
}
