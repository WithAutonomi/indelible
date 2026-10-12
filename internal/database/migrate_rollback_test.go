package database_test

import (
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
)

// TestMigrationRollback_DropsHostedTransactionRows proves migration 016's
// Down completes with hosted-payment rows present (wallet_id NULL) on both
// drivers — under SQLite's foreign_keys pragma the previous COALESCE(wallet_id,
// 0) rewrite failed, and on Postgres SET NOT NULL fails on NULLs. The rows are
// dropped, deliberately (#163 review, V2-1269).
func TestMigrationRollback_DropsHostedTransactionRows(t *testing.T) {
	db := dbtest.OpenEmptyDB(t)
	driver := db.Driver()
	if err := database.Migrate(db, driver); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO transactions (wallet_id, upload_id, tx_type, amount, balance_after, tx_hash) VALUES (NULL, NULL, 'hosted_payment', '1', '0', '0xabc')`); err != nil {
		t.Fatalf("insert hosted row at head schema: %v", err)
	}

	// Roll back past 016 (head is 017: gateway_fee, then 016: wallet-less).
	for i := 0; i < 2; i++ {
		if err := database.MigrateDown(db, driver); err != nil {
			t.Fatalf("migrate down step %d: %v", i+1, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&n); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if n != 0 {
		t.Fatalf("hosted rows must be dropped on rollback (pre-016 schema cannot hold them), found %d", n)
	}
	// And the column is NOT NULL again: a NULL insert must now be refused.
	if _, err := db.Exec(`INSERT INTO transactions (wallet_id, upload_id, tx_type, amount, balance_after) VALUES (NULL, NULL, 'x', '1', '0')`); err == nil || !strings.Contains(strings.ToLower(err.Error()), "null") {
		t.Fatalf("post-rollback schema must reject NULL wallet_id, got err=%v", err)
	}
	if err := database.Migrate(db, driver); err != nil {
		t.Fatalf("migrate up again after rollback: %v", err)
	}
}
