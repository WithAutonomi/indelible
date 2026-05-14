package services

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// createTestUser is a helper that creates a user and fails the test on error.
func createTestUser(t *testing.T, svc *UserService, email, firstName, lastName string) *User {
	t.Helper()
	u, err := svc.Create(email, "hashed_pw", firstName, lastName)
	if err != nil {
		t.Fatalf("createTestUser(%s): %v", email, err)
	}
	return u
}
