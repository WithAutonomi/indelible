package services

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
)

func setupTestDB(t *testing.T) *database.DB {
	return dbtest.OpenDB(t)
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
