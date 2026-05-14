package services

import (
	"testing"
)

// ---------------------------------------------------------------------------
// SetDirect / GetDirect
// ---------------------------------------------------------------------------

func TestPermissionSetDirect_AndGetDirect(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	target := createTestUser(t, userSvc, "target@example.com", "Target", "User")

	if err := permSvc.SetDirect(target.ID, "read", admin.ID); err != nil {
		t.Fatalf("SetDirect(read): %v", err)
	}

	level, err := permSvc.GetDirect(target.ID)
	if err != nil {
		t.Fatalf("GetDirect: %v", err)
	}
	if level != "read" {
		t.Errorf("level = %q, want read", level)
	}
}

func TestPermissionSetDirect_Upsert(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	target := createTestUser(t, userSvc, "target@example.com", "Target", "User")

	// Set read first
	if err := permSvc.SetDirect(target.ID, "read", admin.ID); err != nil {
		t.Fatalf("SetDirect(read): %v", err)
	}

	// Upsert to write
	if err := permSvc.SetDirect(target.ID, "write", admin.ID); err != nil {
		t.Fatalf("SetDirect(write): %v", err)
	}

	level, err := permSvc.GetDirect(target.ID)
	if err != nil {
		t.Fatalf("GetDirect: %v", err)
	}
	if level != "write" {
		t.Errorf("level = %q, want write after upsert", level)
	}
}

func TestPermissionGetDirect_NoPermission(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	user := createTestUser(t, userSvc, "noperm@example.com", "No", "Perm")

	level, err := permSvc.GetDirect(user.ID)
	if err != nil {
		t.Fatalf("GetDirect: %v", err)
	}
	if level != "" {
		t.Errorf("level = %q, want empty string for no permission", level)
	}
}

// ---------------------------------------------------------------------------
// GetEffective (direct only, no group)
// ---------------------------------------------------------------------------

func TestPermissionGetEffective_DirectOnly(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	target := createTestUser(t, userSvc, "target@example.com", "Target", "User")

	if err := permSvc.SetDirect(target.ID, "write", admin.ID); err != nil {
		t.Fatalf("SetDirect: %v", err)
	}

	eff, err := permSvc.GetEffective(target.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "write" {
		t.Errorf("effective = %q, want write", eff)
	}
}

func TestPermissionGetEffective_NoPermission(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	user := createTestUser(t, userSvc, "none@example.com", "No", "Perms")

	eff, err := permSvc.GetEffective(user.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "" {
		t.Errorf("effective = %q, want empty", eff)
	}
}

// ---------------------------------------------------------------------------
// GetEffective (with group inheritance)
// ---------------------------------------------------------------------------

func TestPermissionGetEffective_GroupInheritance(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	user := createTestUser(t, userSvc, "user@example.com", "Regular", "User")

	// Create group with "write" permission
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"writers", "Writer group", "write",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	// Add user to group
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, user.ID, admin.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	// No direct permission -- effective should come from group
	eff, err := permSvc.GetEffective(user.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "write" {
		t.Errorf("effective = %q, want write (from group)", eff)
	}
}

func TestPermissionGetEffective_DirectHigherThanGroup(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	user := createTestUser(t, userSvc, "user@example.com", "Regular", "User")

	// Group gives "read"
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"readers", "Reader group", "read",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, user.ID, admin.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	// Direct gives "admin"
	if err := permSvc.SetDirect(user.ID, "admin", admin.ID); err != nil {
		t.Fatalf("SetDirect: %v", err)
	}

	eff, err := permSvc.GetEffective(user.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "admin" {
		t.Errorf("effective = %q, want admin (direct > group)", eff)
	}
}

func TestPermissionGetEffective_GroupHigherThanDirect(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	user := createTestUser(t, userSvc, "user@example.com", "Regular", "User")

	// Direct gives "read"
	if err := permSvc.SetDirect(user.ID, "read", admin.ID); err != nil {
		t.Fatalf("SetDirect: %v", err)
	}

	// Group gives "admin"
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"admins", "Admin group", "admin",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, user.ID, admin.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	eff, err := permSvc.GetEffective(user.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "admin" {
		t.Errorf("effective = %q, want admin (group > direct)", eff)
	}
}

func TestPermissionGetEffective_InactiveGroupIgnored(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	user := createTestUser(t, userSvc, "user@example.com", "Regular", "User")

	// Create inactive group with "admin"
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level, is_active) VALUES (?, ?, ?, FALSE) RETURNING id`,
		"inactive-admins", "Inactive admin group", "admin",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, user.ID, admin.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	// Direct gives "read"
	if err := permSvc.SetDirect(user.ID, "read", admin.ID); err != nil {
		t.Fatalf("SetDirect: %v", err)
	}

	eff, err := permSvc.GetEffective(user.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "read" {
		t.Errorf("effective = %q, want read (inactive group should be ignored)", eff)
	}
}

// ---------------------------------------------------------------------------
// IsAdmin
// ---------------------------------------------------------------------------

func TestPermissionIsAdmin_True(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	if err := permSvc.SetDirect(admin.ID, "admin", admin.ID); err != nil {
		t.Fatalf("SetDirect: %v", err)
	}

	isAdmin, err := permSvc.IsAdmin(admin.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if !isAdmin {
		t.Error("expected IsAdmin = true")
	}
}

func TestPermissionIsAdmin_FalseForWrite(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	admin := createTestUser(t, userSvc, "admin@example.com", "Admin", "User")
	writer := createTestUser(t, userSvc, "writer@example.com", "Writer", "User")
	if err := permSvc.SetDirect(writer.ID, "write", admin.ID); err != nil {
		t.Fatalf("SetDirect: %v", err)
	}

	isAdmin, err := permSvc.IsAdmin(writer.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if isAdmin {
		t.Error("expected IsAdmin = false for write-level user")
	}
}

func TestPermissionIsAdmin_FalseForNoPermission(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	user := createTestUser(t, userSvc, "nobody@example.com", "No", "Body")

	isAdmin, err := permSvc.IsAdmin(user.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if isAdmin {
		t.Error("expected IsAdmin = false for user with no permissions")
	}
}

func TestPermissionIsAdmin_ViaGroup(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	granter := createTestUser(t, userSvc, "granter@example.com", "Granter", "User")
	user := createTestUser(t, userSvc, "user@example.com", "Regular", "User")

	// Admin group
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"admin-group", "Admin group", "admin",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, user.ID, granter.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	isAdmin, err := permSvc.IsAdmin(user.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if !isAdmin {
		t.Error("expected IsAdmin = true via group membership")
	}
}

// ---------------------------------------------------------------------------
// CountAdmins
// ---------------------------------------------------------------------------

func TestPermissionCountAdmins_DirectOnly(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	a1 := createTestUser(t, userSvc, "admin1@example.com", "Admin", "One")
	a2 := createTestUser(t, userSvc, "admin2@example.com", "Admin", "Two")
	writer := createTestUser(t, userSvc, "writer@example.com", "Writer", "User")

	permSvc.SetDirect(a1.ID, "admin", a1.ID)
	permSvc.SetDirect(a2.ID, "admin", a1.ID)
	permSvc.SetDirect(writer.ID, "write", a1.ID)

	count, err := permSvc.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestPermissionCountAdmins_IncludesGroupAdmins(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	directAdmin := createTestUser(t, userSvc, "direct@example.com", "Direct", "Admin")
	groupAdmin := createTestUser(t, userSvc, "group@example.com", "Group", "Admin")

	permSvc.SetDirect(directAdmin.ID, "admin", directAdmin.ID)

	// Group admin
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"admin-grp", "Admin group", "admin",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, groupAdmin.ID, directAdmin.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	count, err := permSvc.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (1 direct + 1 group)", count)
	}
}

func TestPermissionCountAdmins_NoDuplicatesForBothPaths(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	user := createTestUser(t, userSvc, "both@example.com", "Both", "Paths")

	// Direct admin
	permSvc.SetDirect(user.ID, "admin", user.ID)

	// Also in admin group
	var groupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"admin-grp", "Admin group", "admin",
	).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, user.ID, user.ID,
	)
	if err != nil {
		t.Fatalf("add to group: %v", err)
	}

	count, err := permSvc.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (DISTINCT should prevent duplicates)", count)
	}
}

func TestPermissionCountAdmins_ExcludesInactiveUsers(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	active := createTestUser(t, userSvc, "active@example.com", "Active", "Admin")
	inactive := createTestUser(t, userSvc, "inactive@example.com", "Inactive", "Admin")

	permSvc.SetDirect(active.ID, "admin", active.ID)
	permSvc.SetDirect(inactive.ID, "admin", active.ID)

	// Deactivate one
	f := false
	if err := userSvc.Update(inactive.ID, "", "", &f); err != nil {
		t.Fatalf("Update: %v", err)
	}

	count, err := permSvc.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (inactive user excluded)", count)
	}
}

func TestPermissionCountAdmins_Zero(t *testing.T) {
	db := setupTestDB(t)
	permSvc := NewPermissionService(db)

	count, err := permSvc.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (no users)", count)
	}
}

// ---------------------------------------------------------------------------
// Permission hierarchy (admin > write > read)
// ---------------------------------------------------------------------------

func TestPermissionHierarchy(t *testing.T) {
	tests := []struct {
		a, b string
		want string
	}{
		{"admin", "read", "admin"},
		{"read", "admin", "admin"},
		{"admin", "write", "admin"},
		{"write", "admin", "admin"},
		{"write", "read", "write"},
		{"read", "write", "write"},
		{"admin", "admin", "admin"},
		{"write", "write", "write"},
		{"read", "read", "read"},
		{"", "read", "read"},
		{"read", "", "read"},
		{"", "", ""},
		{"admin", "", "admin"},
		{"", "admin", "admin"},
	}

	for _, tc := range tests {
		got := highest(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("highest(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Multiple groups: highest wins
// ---------------------------------------------------------------------------

func TestPermissionGetEffective_MultipleGroups(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	permSvc := NewPermissionService(db)

	granter := createTestUser(t, userSvc, "granter@example.com", "Granter", "User")
	user := createTestUser(t, userSvc, "user@example.com", "Multi", "Group")

	// Create read group
	var readGroupID int64
	err := db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"readers", "Reader group", "read",
	).Scan(&readGroupID)
	if err != nil {
		t.Fatalf("create read group: %v", err)
	}

	// Create write group
	var writeGroupID int64
	err = db.QueryRow(
		`INSERT INTO groups (name, description, permission_level) VALUES (?, ?, ?) RETURNING id`,
		"writers", "Writer group", "write",
	).Scan(&writeGroupID)
	if err != nil {
		t.Fatalf("create write group: %v", err)
	}

	// Add user to both groups
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		readGroupID, user.ID, granter.ID,
	)
	if err != nil {
		t.Fatalf("add to read group: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		writeGroupID, user.ID, granter.ID,
	)
	if err != nil {
		t.Fatalf("add to write group: %v", err)
	}

	eff, err := permSvc.GetEffective(user.ID)
	if err != nil {
		t.Fatalf("GetEffective: %v", err)
	}
	if eff != "write" {
		t.Errorf("effective = %q, want write (highest of read, write)", eff)
	}
}
