package database

import "testing"

func TestMigrationRoundTrip(t *testing.T) {
	db, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Migrate up
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Verify tables exist
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`).Scan(&count)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if count == 0 {
		t.Fatal("expected tables after migration up, got 0")
	}
	t.Logf("tables after up: %d", count)

	// Migrate down
	if err := MigrateDown(db, "sqlite"); err != nil {
		t.Fatalf("migrate down: %v", err)
	}

	// Verify tables dropped
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`).Scan(&count)
	if err != nil {
		t.Fatalf("count tables after down: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tables after migration down, got %d", count)
	}

	// Migrate up again
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate up (second): %v", err)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`).Scan(&count)
	if err != nil {
		t.Fatalf("count tables after second up: %v", err)
	}
	if count == 0 {
		t.Fatal("expected tables after second migration up, got 0")
	}
	t.Logf("tables after round-trip: %d", count)
}
