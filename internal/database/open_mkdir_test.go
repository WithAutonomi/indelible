package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// V2-302: bare-binary first-run on a fresh non-root box used to crash with
// "out of memory (14)" because the parent dir of the SQLite file didn't
// exist. Open should MkdirAll silently, and on the residual CANTOPEN paths
// (read-only mount, unwritable parent) it should surface a hint about
// INDELIBLE_DB_URL / INDELIBLE_DATA_DIR instead of the driver wording.

func TestOpen_AutoCreatesMissingParentDirectory(t *testing.T) {
	tmp := t.TempDir()
	// Use a nested path so the parent definitely doesn't exist yet.
	dbPath := filepath.Join(tmp, "nested", "deeper", "data.db")
	url := "sqlite://" + dbPath

	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open with missing parent dir: %v", err)
	}
	defer db.Close()

	// The file is created lazily by SQLite once it writes; what we care about
	// is that Open itself didn't crash on the unwritable-dir path.
	if _, statErr := filepath.Glob(filepath.Dir(dbPath)); statErr != nil {
		t.Fatalf("parent dir not created: %v", statErr)
	}
}

func TestSqliteFilePath(t *testing.T) {
	cases := []struct {
		dsn  string
		want string
	}{
		{"/var/lib/indelible/data.db", "/var/lib/indelible/data.db"},
		{"file:/tmp/x.db", "/tmp/x.db"},
		{"file:/tmp/x.db?mode=rwc", "/tmp/x.db"},
		{":memory:", ""},
		{"file:memdb5?mode=memory&cache=shared", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := sqliteFilePath(c.dsn)
		if got != c.want {
			t.Errorf("sqliteFilePath(%q) = %q, want %q", c.dsn, got, c.want)
		}
	}
}

func TestSqliteOpenError_CantOpenIsTranslated(t *testing.T) {
	// modernc.org/sqlite renders SQLITE_CANTOPEN as "out of memory (14)".
	driverErr := errors.New("unable to open database file: out of memory (14)")
	dsn := filepath.Join("no", "such", "dir", "data.db")
	out := sqliteOpenError(driverErr, dsn)
	if out == nil {
		t.Fatal("expected an error, got nil")
	}
	msg := out.Error()
	// Surfaces a hint about INDELIBLE_DB_URL / INDELIBLE_DATA_DIR
	if !strings.Contains(msg, "INDELIBLE_DB_URL") || !strings.Contains(msg, "INDELIBLE_DATA_DIR") {
		t.Errorf("translated error missing env-var hint: %q", msg)
	}
	// Includes the offending directory so the operator knows what to fix.
	// filepath.Dir uses native separators (\ on Windows, / on Unix); just
	// check for the last path component.
	if !strings.Contains(msg, "dir") {
		t.Errorf("translated error missing dir: %q", msg)
	}
	// Preserves the driver detail for diagnostics
	if !strings.Contains(msg, "(14)") {
		t.Errorf("translated error dropped driver detail: %q", msg)
	}
}

func TestSqliteOpenError_OtherErrorsPassThrough(t *testing.T) {
	other := errors.New("disk I/O error (10)")
	out := sqliteOpenError(other, "/tmp/x.db")
	if !strings.Contains(out.Error(), "disk I/O error") {
		t.Errorf("non-CANTOPEN errors should pass through verbatim, got %q", out)
	}
	if strings.Contains(out.Error(), "INDELIBLE_DB_URL") {
		t.Errorf("non-CANTOPEN errors should not get the hint: %q", out)
	}
}
