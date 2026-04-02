package services

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestUserCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u, err := svc.Create("alice@example.com", "hashed_pw", "Alice", "Smith")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", u.Email)
	}
	if u.FirstName != "Alice" || u.LastName != "Smith" {
		t.Errorf("name = %q %q, want Alice Smith", u.FirstName, u.LastName)
	}
	if !u.IsActive {
		t.Error("expected IsActive to default to true")
	}
	if u.IsServiceAccount {
		t.Error("expected IsServiceAccount to default to false")
	}
	if u.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if u.DeletedAt.Valid {
		t.Error("expected DeletedAt to be NULL")
	}
}

func TestUserCreate_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	_, err := svc.Create("dup@example.com", "hash1", "First", "User")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = svc.Create("dup@example.com", "hash2", "Second", "User")
	if err != ErrEmailTaken {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestUserGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	created := createTestUser(t, svc, "get@example.com", "Get", "User")

	found, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.Email != created.Email {
		t.Errorf("email = %q, want %q", found.Email, created.Email)
	}
}

func TestUserGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	_, err := svc.GetByID(999)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserGetByID_SoftDeletedReturnsNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "deleted@example.com", "Del", "User")
	if err := svc.SoftDelete(u.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	_, err := svc.GetByID(u.ID)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for soft-deleted user, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetByEmail
// ---------------------------------------------------------------------------

func TestUserGetByEmail(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	createTestUser(t, svc, "lookup@example.com", "Lookup", "User")

	found, err := svc.GetByEmail("lookup@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if found.FirstName != "Lookup" {
		t.Errorf("FirstName = %q, want Lookup", found.FirstName)
	}
}

func TestUserGetByEmail_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	_, err := svc.GetByEmail("nonexistent@example.com")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserGetByEmail_SoftDeletedExcluded(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "softdel@example.com", "Soft", "Del")
	if err := svc.SoftDelete(u.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	_, err := svc.GetByEmail("softdel@example.com")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for soft-deleted, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestUserList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	createTestUser(t, svc, "a@example.com", "A", "User")
	createTestUser(t, svc, "b@example.com", "B", "User")
	createTestUser(t, svc, "c@example.com", "C", "User")

	users, total, err := svc.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(users) != 3 {
		t.Errorf("len(users) = %d, want 3", len(users))
	}
}

func TestUserList_Pagination(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	for i := 0; i < 5; i++ {
		createTestUser(t, svc, "page"+string(rune('0'+i))+"@example.com", "User", "Page")
	}

	// First page
	users, total, err := svc.List(2, 0)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(users) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(users))
	}

	// Second page
	users2, _, err := svc.List(2, 2)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(users2) != 2 {
		t.Errorf("page 2 len = %d, want 2", len(users2))
	}
}

func TestUserList_ExcludesSoftDeleted(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	createTestUser(t, svc, "keep@example.com", "Keep", "Me")
	del := createTestUser(t, svc, "remove@example.com", "Remove", "Me")
	if err := svc.SoftDelete(del.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	users, total, err := svc.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(users) != 1 {
		t.Errorf("len = %d, want 1", len(users))
	}
	if users[0].Email != "keep@example.com" {
		t.Errorf("email = %q, want keep@example.com", users[0].Email)
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUserUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "update@example.com", "Old", "Name")

	if err := svc.Update(u.ID, "New", "Last", nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, err := svc.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if updated.FirstName != "New" {
		t.Errorf("FirstName = %q, want New", updated.FirstName)
	}
	if updated.LastName != "Last" {
		t.Errorf("LastName = %q, want Last", updated.LastName)
	}
}

func TestUserUpdate_IsActive(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "toggle@example.com", "Toggle", "User")

	active := false
	if err := svc.Update(u.ID, "", "", &active); err != nil {
		t.Fatalf("Update(active=false): %v", err)
	}

	updated, err := svc.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if updated.IsActive {
		t.Error("expected IsActive = false after deactivation")
	}

	// Reactivate
	active = true
	if err := svc.Update(u.ID, "", "", &active); err != nil {
		t.Fatalf("Update(active=true): %v", err)
	}
	updated, _ = svc.GetByID(u.ID)
	if !updated.IsActive {
		t.Error("expected IsActive = true after reactivation")
	}
}

func TestUserUpdate_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	err := svc.Update(999, "X", "Y", nil)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserUpdate_PartialOnlyFirstName(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "partial@example.com", "Original", "Last")

	// Only update first name -- empty lastName means keep existing
	if err := svc.Update(u.ID, "Changed", "", nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, _ := svc.GetByID(u.ID)
	if updated.FirstName != "Changed" {
		t.Errorf("FirstName = %q, want Changed", updated.FirstName)
	}
	if updated.LastName != "Last" {
		t.Errorf("LastName = %q, want Last (unchanged)", updated.LastName)
	}
}

// ---------------------------------------------------------------------------
// SoftDelete
// ---------------------------------------------------------------------------

func TestUserSoftDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "todelete@example.com", "Del", "Me")
	if err := svc.SoftDelete(u.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// Verify not findable by normal queries
	_, err := svc.GetByID(u.ID)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	// Verify count is correct
	count, err := svc.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// UpdatePassword
// ---------------------------------------------------------------------------

func TestUserUpdatePassword(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "pwchange@example.com", "PW", "User")

	// Initially password_changed_at should be NULL
	if u.PasswordChangedAt.Valid {
		t.Error("expected PasswordChangedAt to be NULL initially")
	}

	if err := svc.UpdatePassword(u.ID, "new_hash_value"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}

	updated, err := svc.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID after password change: %v", err)
	}
	if !updated.PasswordChangedAt.Valid {
		t.Error("expected PasswordChangedAt to be set after UpdatePassword")
	}
	if updated.PasswordHash.String != "new_hash_value" {
		t.Errorf("PasswordHash = %q, want new_hash_value", updated.PasswordHash.String)
	}
}

// ---------------------------------------------------------------------------
// CreateServiceAccount
// ---------------------------------------------------------------------------

func TestUserCreateServiceAccount(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u, err := svc.CreateServiceAccount("svc@example.com", "Service", "Bot")
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if !u.IsServiceAccount {
		t.Error("expected IsServiceAccount = true")
	}
	if u.PasswordHash.Valid && u.PasswordHash.String != "" {
		t.Error("expected no password for service account")
	}
	if u.Email != "svc@example.com" {
		t.Errorf("email = %q, want svc@example.com", u.Email)
	}
}

func TestUserCreateServiceAccount_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	_, err := svc.CreateServiceAccount("svc@example.com", "First", "Svc")
	if err != nil {
		t.Fatalf("first CreateServiceAccount: %v", err)
	}

	_, err = svc.CreateServiceAccount("svc@example.com", "Second", "Svc")
	if err != ErrEmailTaken {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CreateFromSCIM
// ---------------------------------------------------------------------------

func TestUserCreateFromSCIM(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u, err := svc.CreateFromSCIM("scim@example.com", "Scim", "User", "ext-123")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}
	if u.Email != "scim@example.com" {
		t.Errorf("email = %q, want scim@example.com", u.Email)
	}
	if !u.ExternalID.Valid || u.ExternalID.String != "ext-123" {
		t.Errorf("ExternalID = %v, want ext-123", u.ExternalID)
	}
	if u.IsServiceAccount {
		t.Error("SCIM user should not be a service account")
	}
	if u.PasswordHash.Valid && u.PasswordHash.String != "" {
		t.Error("SCIM user should have no password")
	}
}

func TestUserCreateFromSCIM_EmptyExternalID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u, err := svc.CreateFromSCIM("scim2@example.com", "Scim", "Two", "")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}
	if u.ExternalID.Valid {
		t.Error("expected ExternalID to be NULL when empty string provided")
	}
}

func TestUserCreateFromSCIM_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	_, err := svc.CreateFromSCIM("dup@example.com", "One", "User", "ext-1")
	if err != nil {
		t.Fatalf("first CreateFromSCIM: %v", err)
	}

	_, err = svc.CreateFromSCIM("dup@example.com", "Two", "User", "ext-2")
	if err != ErrEmailTaken {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetByExternalID
// ---------------------------------------------------------------------------

func TestUserGetByExternalID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	created, err := svc.CreateFromSCIM("ext@example.com", "Ext", "User", "ext-abc")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}

	found, err := svc.GetByExternalID("ext-abc")
	if err != nil {
		t.Fatalf("GetByExternalID: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %d, want %d", found.ID, created.ID)
	}
	if found.Email != "ext@example.com" {
		t.Errorf("Email = %q, want ext@example.com", found.Email)
	}
}

func TestUserGetByExternalID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	_, err := svc.GetByExternalID("nonexistent")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserGetByExternalID_SoftDeletedExcluded(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u, err := svc.CreateFromSCIM("ext-del@example.com", "Ext", "Del", "ext-del")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}
	if err := svc.SoftDelete(u.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	_, err = svc.GetByExternalID("ext-del")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for soft-deleted, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Count
// ---------------------------------------------------------------------------

func TestUserCount(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	count, err := svc.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	createTestUser(t, svc, "one@example.com", "One", "User")
	createTestUser(t, svc, "two@example.com", "Two", "User")

	count, err = svc.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

// ---------------------------------------------------------------------------
// UpdateLastLogin
// ---------------------------------------------------------------------------

func TestUserUpdateLastLogin(t *testing.T) {
	db := setupTestDB(t)
	svc := NewUserService(db)

	u := createTestUser(t, svc, "login@example.com", "Login", "User")
	if u.LastLoginAt.Valid {
		t.Error("expected LastLoginAt to be NULL initially")
	}

	if err := svc.UpdateLastLogin(u.ID); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}

	updated, err := svc.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !updated.LastLoginAt.Valid {
		t.Error("expected LastLoginAt to be set after UpdateLastLogin")
	}
}
