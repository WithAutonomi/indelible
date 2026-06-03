package services_test

import (
	"sync"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/services"
)

func seedCfg() *config.Config {
	return &config.Config{
		AdminEmail:    "boss@example.com",
		AdminPassword: "supersecret123",
	}
}

func TestSeedAdmin_CreatesFirstAdmin(t *testing.T) {
	db := dbtest.OpenDB(t)
	cfg := seedCfg()

	seeded, err := services.SeedAdmin(db, cfg)
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if !seeded {
		t.Fatal("expected SeedAdmin to create the admin")
	}

	permSvc := services.NewPermissionService(db)
	n, err := permSvc.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if n != 1 {
		t.Fatalf("admin count = %d, want 1", n)
	}

	user, err := services.NewUserService(db).GetByEmail(cfg.AdminEmail)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if level, _ := permSvc.GetEffective(user.ID); level != "admin" {
		t.Errorf("seeded user level = %q, want admin", level)
	}
}

func TestSeedAdmin_NoCredsIsNoop(t *testing.T) {
	db := dbtest.OpenDB(t)

	seeded, err := services.SeedAdmin(db, &config.Config{})
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if seeded {
		t.Fatal("expected no-op with no creds configured")
	}
	if n, _ := services.NewPermissionService(db).CountAdmins(); n != 0 {
		t.Fatalf("admin count = %d, want 0", n)
	}
}

func TestSeedAdmin_IdempotentAcrossRestarts(t *testing.T) {
	db := dbtest.OpenDB(t)
	cfg := seedCfg()

	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("first SeedAdmin: %v", err)
	}
	// Second boot — must not create a second admin or error.
	seeded, err := services.SeedAdmin(db, cfg)
	if err != nil {
		t.Fatalf("second SeedAdmin: %v", err)
	}
	if seeded {
		t.Fatal("expected create-only no-op on second call")
	}
	if n, _ := services.NewPermissionService(db).CountAdmins(); n != 1 {
		t.Fatalf("admin count = %d, want 1", n)
	}
}

// TestSeedAdmin_SkipsWhenOtherAdminExists verifies create-only semantics: a
// pre-existing admin under a different email means the configured seed email
// is never created.
func TestSeedAdmin_SkipsWhenOtherAdminExists(t *testing.T) {
	db := dbtest.OpenDB(t)

	// Make a different admin out of band.
	other, err := services.NewUserService(db).Create("first@example.com", "x", "First", "Admin")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := services.NewPermissionService(db).SetDirect(other.ID, "admin", other.ID); err != nil {
		t.Fatalf("grant other admin: %v", err)
	}

	seeded, err := services.SeedAdmin(db, seedCfg())
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if seeded {
		t.Fatal("expected no-op when an admin already exists")
	}
	if _, err := services.NewUserService(db).GetByEmail("boss@example.com"); err == nil {
		t.Fatal("seed email should not have been created when an admin already exists")
	}
}

// TestSeedAdmin_ConcurrentSeedYieldsSingleAdmin exercises the multi-replica
// startup race: many concurrent SeedAdmin calls must yield exactly one admin,
// never zero, never two.
func TestSeedAdmin_ConcurrentSeedYieldsSingleAdmin(t *testing.T) {
	db := dbtest.OpenDB(t)
	cfg := seedCfg()

	const n = 8
	var wg sync.WaitGroup
	created := make([]bool, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			created[i], errs[i] = services.SeedAdmin(db, cfg)
		}(i)
	}
	wg.Wait()

	createdCount := 0
	for i := range n {
		if errs[i] != nil {
			t.Errorf("goroutine %d errored: %v", i, errs[i])
		}
		if created[i] {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("created-true count = %d, want exactly 1", createdCount)
	}

	if got, _ := services.NewPermissionService(db).CountAdmins(); got != 1 {
		t.Fatalf("final admin count = %d, want 1", got)
	}
}
