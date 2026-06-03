package services

import (
	"errors"
	"fmt"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
)

// SeedAdmin creates the bootstrap administrator from config when the instance
// has no admin yet. Since self-registration is disabled by default and never
// grants admin, this is the only way a fresh instance gets its first admin.
//
// It is create-only and idempotent, so it is safe to call on every boot (the
// container restarts under `restart: unless-stopped`): if any admin already
// exists it is a no-op. Concurrent multi-replica startup is safe too — the
// users.email UNIQUE constraint lets at most one replica win the INSERT; a
// loser sees ErrEmailTaken and returns without creating a second admin. If
// nobody ends up an admin (e.g. the configured email already belongs to a
// non-admin user), the caller's post-seed CountAdmins check surfaces it.
//
// Returns (true, nil) only when it created the admin; (false, nil) when it
// skipped (no creds configured, an admin already exists, or a concurrent seed
// won the race).
func SeedAdmin(db *database.DB, cfg *config.Config) (bool, error) {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return false, nil
	}

	permSvc := NewPermissionService(db)
	n, err := permSvc.CountAdmins()
	if err != nil {
		return false, fmt.Errorf("counting admins: %w", err)
	}
	if n > 0 {
		return false, nil // create-only: an admin already exists
	}

	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return false, fmt.Errorf("hashing seed admin password: %w", err)
	}

	userSvc := NewUserService(db)
	user, err := userSvc.Create(cfg.AdminEmail, hash, "Admin", "User")
	if err != nil {
		// A concurrent replica (or a pre-existing user) already owns this
		// email. Either way we did not create the admin; leave it to the
		// caller's admin-less warning to flag the misconfiguration case.
		if errors.Is(err, ErrEmailTaken) {
			return false, nil
		}
		return false, fmt.Errorf("creating seed admin: %w", err)
	}

	if err := permSvc.SetDirect(user.ID, "admin", user.ID); err != nil {
		return false, fmt.Errorf("granting seed admin permission: %w", err)
	}
	return true, nil
}
