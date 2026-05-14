package database_test

import (
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
)

// tableCount returns the number of application tables (excluding the
// dialect-specific schema catalog and goose's bookkeeping table).
func tableCount(t *testing.T, db *database.DB) int {
	t.Helper()
	var query string
	switch db.Driver() {
	case "sqlite":
		query = `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`
	case "postgres":
		query = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name != 'goose_db_version'`
	default:
		t.Fatalf("unsupported driver: %s", db.Driver())
	}
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	return count
}

func TestMigrationRoundTrip(t *testing.T) {
	db := dbtest.OpenEmptyDB(t)
	driver := db.Driver()

	// Migrate up
	if err := database.Migrate(db, driver); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if c := tableCount(t, db); c == 0 {
		t.Fatal("expected tables after migration up, got 0")
	}

	// MigrateDown rolls back a single migration; loop until goose reports
	// "no current version found" so the round-trip works regardless of how
	// many migrations exist.
	for i := range 100 {
		err := database.MigrateDown(db, driver)
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "no current version found") {
			break
		}
		t.Fatalf("migrate down (step %d): %v", i, err)
	}

	if c := tableCount(t, db); c != 0 {
		t.Errorf("expected 0 tables after migration down, got %d", c)
	}

	// Migrate up again
	if err := database.Migrate(db, driver); err != nil {
		t.Fatalf("migrate up (second): %v", err)
	}
	if c := tableCount(t, db); c == 0 {
		t.Fatal("expected tables after second migration up, got 0")
	}
}
