package database

import (
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

// MigrateDown rolls back one migration for the given driver.
func MigrateDown(db *DB, driver string) error {
	var fs embed.FS
	var dir string

	switch driver {
	case "sqlite":
		fs = sqliteMigrations
		dir = "migrations/sqlite"
	case "postgres":
		fs = postgresMigrations
		dir = "migrations/postgres"
	default:
		return fmt.Errorf("unsupported driver for migrations: %s", driver)
	}

	goose.SetBaseFS(fs)

	if err := goose.SetDialect(driver); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}

	if err := goose.Down(db.DB, dir); err != nil {
		return fmt.Errorf("rolling back migration: %w", err)
	}

	return nil
}

// Migrate runs all pending database migrations for the given driver.
func Migrate(db *DB, driver string) error {
	var fs embed.FS
	var dir string

	switch driver {
	case "sqlite":
		fs = sqliteMigrations
		dir = "migrations/sqlite"
	case "postgres":
		fs = postgresMigrations
		dir = "migrations/postgres"
	default:
		return fmt.Errorf("unsupported driver for migrations: %s", driver)
	}

	goose.SetBaseFS(fs)

	if err := goose.SetDialect(driver); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}

	if err := goose.Up(db.DB, dir); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}
