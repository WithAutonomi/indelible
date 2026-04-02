package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// Open connects to the database specified by the URL.
// Supports sqlite:// and postgres:// schemes.
func Open(dbURL string) (*sql.DB, error) {
	driver, dsn, err := parseURL(dbURL)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite pragmas for performance
	if driver == "sqlite" {
		if _, err := db.Exec(`
			PRAGMA journal_mode=WAL;
			PRAGMA busy_timeout=5000;
			PRAGMA synchronous=NORMAL;
			PRAGMA foreign_keys=ON;
		`); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting SQLite pragmas: %w", err)
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
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return db, nil
}

// parseURL converts a db_url into a driver name and DSN.
func parseURL(dbURL string) (driver, dsn string, err error) {
	switch {
	case strings.HasPrefix(dbURL, "sqlite://"):
		return "sqlite", strings.TrimPrefix(dbURL, "sqlite://"), nil
	case strings.HasPrefix(dbURL, "postgres://"), strings.HasPrefix(dbURL, "postgresql://"):
		return "postgres", dbURL, nil
	default:
		return "", "", fmt.Errorf("unsupported database URL scheme: %s (use sqlite:// or postgres://)", dbURL)
	}
}
