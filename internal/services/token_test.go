package services

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestTokenCreate(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "token@example.com", "Token", "Owner")

	secret, tok, err := tokenSvc.Create(
		user.ID, "My Token", "Test token",
		`["read","write"]`, "", nil, "", nil,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Secret should start with ind_
	if !strings.HasPrefix(secret, "ind_") {
		t.Errorf("secret = %q, expected prefix ind_", secret)
	}
	// ind_ + 64 hex chars = 68 total
	if len(secret) != 68 {
		t.Errorf("len(secret) = %d, want 68", len(secret))
	}

	if tok.ID == 0 {
		t.Fatal("expected non-zero token ID")
	}
	if tok.Name != "My Token" {
		t.Errorf("Name = %q, want My Token", tok.Name)
	}
	if tok.Description != "Test token" {
		t.Errorf("Description = %q, want Test token", tok.Description)
	}
	if tok.UserID != user.ID {
		t.Errorf("UserID = %d, want %d", tok.UserID, user.ID)
	}
	if tok.Permissions != `["read","write"]` {
		t.Errorf("Permissions = %q, want [\"read\",\"write\"]", tok.Permissions)
	}
	if tok.UUID == "" {
		t.Error("expected UUID to be set")
	}
	if tok.UsageCount != 0 {
		t.Errorf("UsageCount = %d, want 0", tok.UsageCount)
	}
	if tok.RevokedAt.Valid {
		t.Error("expected RevokedAt to be NULL")
	}
	if tok.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestTokenCreate_DefaultPermissions(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "default@example.com", "Default", "Perms")

	_, tok, err := tokenSvc.Create(
		user.ID, "Default Token", "", "", "", nil, "", nil,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tok.Permissions != `["read"]` {
		t.Errorf("Permissions = %q, want [\"read\"] (default)", tok.Permissions)
	}
}

func TestTokenCreate_WithExpiry(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "expiry@example.com", "Expiry", "User")
	future := time.Now().Add(24 * time.Hour)

	_, tok, err := tokenSvc.Create(
		user.ID, "Expiring Token", "", `["read"]`, "", nil, "", &future,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !tok.ExpiresAt.Valid {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestTokenCreate_WithDepartmentAndLimits(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "dept@example.com", "Dept", "User")
	maxSize := int64(1048576)

	_, tok, err := tokenSvc.Create(
		user.ID, "Dept Token", "With limits",
		`["read"]`, "engineering", &maxSize, `["application/json"]`, nil,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !tok.Department.Valid || tok.Department.String != "engineering" {
		t.Errorf("Department = %v, want engineering", tok.Department)
	}
	if !tok.MaxFileSizeBytes.Valid || tok.MaxFileSizeBytes.Int64 != 1048576 {
		t.Errorf("MaxFileSizeBytes = %v, want 1048576", tok.MaxFileSizeBytes)
	}
	if !tok.AllowedFileTypes.Valid || tok.AllowedFileTypes.String != `["application/json"]` {
		t.Errorf("AllowedFileTypes = %v, want [\"application/json\"]", tok.AllowedFileTypes)
	}
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestTokenGetByID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "get@example.com", "Get", "User")
	_, created, _ := tokenSvc.Create(user.ID, "GetMe", "", `["read"]`, "", nil, "", nil)

	found, err := tokenSvc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.UUID != created.UUID {
		t.Errorf("UUID = %q, want %q", found.UUID, created.UUID)
	}
}

func TestTokenGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	tokenSvc := NewTokenService(db)

	_, err := tokenSvc.GetByID(99999)
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetByUUID
// ---------------------------------------------------------------------------

func TestTokenGetByUUID(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "uuid@example.com", "UUID", "User")
	_, created, _ := tokenSvc.Create(user.ID, "UUID Token", "", `["read"]`, "", nil, "", nil)

	found, err := tokenSvc.GetByUUID(created.UUID)
	if err != nil {
		t.Fatalf("GetByUUID: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %d, want %d", found.ID, created.ID)
	}
	if found.Name != "UUID Token" {
		t.Errorf("Name = %q, want UUID Token", found.Name)
	}
}

func TestTokenGetByUUID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	tokenSvc := NewTokenService(db)

	_, err := tokenSvc.GetByUUID("nonexistent-uuid")
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateSecret
// ---------------------------------------------------------------------------

func TestTokenValidateSecret(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "validate@example.com", "Val", "User")
	secret, created, _ := tokenSvc.Create(user.ID, "Valid Token", "", `["read"]`, "", nil, "", nil)

	found, err := tokenSvc.ValidateSecret(secret)
	if err != nil {
		t.Fatalf("ValidateSecret: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %d, want %d", found.ID, created.ID)
	}
}

func TestTokenValidateSecret_WrongSecret(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "wrong@example.com", "Wrong", "User")
	tokenSvc.Create(user.ID, "Some Token", "", `["read"]`, "", nil, "", nil)

	_, err := tokenSvc.ValidateSecret("ind_wrong_secret_value_0000000000000000000000000000000")
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound for wrong secret, got %v", err)
	}
}

func TestTokenValidateSecret_ExpiredToken(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "expired@example.com", "Expired", "User")
	past := time.Now().Add(-1 * time.Hour) // expired 1 hour ago
	secret, _, _ := tokenSvc.Create(user.ID, "Expired Token", "", `["read"]`, "", nil, "", &past)

	_, err := tokenSvc.ValidateSecret(secret)
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound for expired token, got %v", err)
	}
}

func TestTokenValidateSecret_RevokedToken(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "revoked@example.com", "Revoked", "User")
	secret, created, _ := tokenSvc.Create(user.ID, "Revoked Token", "", `["read"]`, "", nil, "", nil)

	// Revoke it
	if err := tokenSvc.Revoke(created.ID, user.ID, "testing"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, err := tokenSvc.ValidateSecret(secret)
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound for revoked token, got %v", err)
	}
}

func TestTokenValidateSecret_NoTokensExist(t *testing.T) {
	db := setupTestDB(t)
	tokenSvc := NewTokenService(db)

	_, err := tokenSvc.ValidateSecret("ind_anything_at_all_000000000000000000000000000000000")
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound when no tokens exist, got %v", err)
	}
}

func TestTokenValidateSecret_MultipleTokensFindsCorrect(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "multi@example.com", "Multi", "User")

	tokenSvc.Create(user.ID, "Token 1", "", `["read"]`, "", nil, "", nil)
	secret2, tok2, _ := tokenSvc.Create(user.ID, "Token 2", "", `["write"]`, "", nil, "", nil)
	tokenSvc.Create(user.ID, "Token 3", "", `["read"]`, "", nil, "", nil)

	found, err := tokenSvc.ValidateSecret(secret2)
	if err != nil {
		t.Fatalf("ValidateSecret: %v", err)
	}
	if found.ID != tok2.ID {
		t.Errorf("ID = %d, want %d (token 2)", found.ID, tok2.ID)
	}
	if found.Name != "Token 2" {
		t.Errorf("Name = %q, want Token 2", found.Name)
	}
}

// ---------------------------------------------------------------------------
// ListByUser
// ---------------------------------------------------------------------------

func TestTokenListByUser(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user1 := createTestUser(t, userSvc, "user1@example.com", "User", "One")
	user2 := createTestUser(t, userSvc, "user2@example.com", "User", "Two")

	tokenSvc.Create(user1.ID, "U1 Token 1", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Create(user1.ID, "U1 Token 2", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Create(user2.ID, "U2 Token 1", "", `["read"]`, "", nil, "", nil)

	tokens, err := tokenSvc.ListByUser(user1.ID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("len = %d, want 2", len(tokens))
	}
	for _, tok := range tokens {
		if tok.UserID != user1.ID {
			t.Errorf("token %d belongs to user %d, want %d", tok.ID, tok.UserID, user1.ID)
		}
	}
}

func TestTokenListByUser_IncludesRevoked(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "audit@example.com", "Audit", "User")

	_, active, _ := tokenSvc.Create(user.ID, "Active", "", `["read"]`, "", nil, "", nil)
	_, revoked, _ := tokenSvc.Create(user.ID, "Revoked", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Revoke(revoked.ID, user.ID, "testing")

	tokens, err := tokenSvc.ListByUser(user.ID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("len = %d, want 2 (includes revoked for audit)", len(tokens))
	}

	// Verify one is revoked and one is not
	foundActive, foundRevoked := false, false
	for _, tok := range tokens {
		if tok.ID == active.ID && !tok.RevokedAt.Valid {
			foundActive = true
		}
		if tok.ID == revoked.ID && tok.RevokedAt.Valid {
			foundRevoked = true
		}
	}
	if !foundActive {
		t.Error("expected to find active token in list")
	}
	if !foundRevoked {
		t.Error("expected to find revoked token in list")
	}
}

func TestTokenListByUser_Empty(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "empty@example.com", "Empty", "User")

	tokens, err := tokenSvc.ListByUser(user.ID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(tokens) != 0 {
		t.Errorf("len = %d, want 0", len(tokens))
	}
}

// ---------------------------------------------------------------------------
// Revoke
// ---------------------------------------------------------------------------

func TestTokenRevoke(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "revoke@example.com", "Revoke", "User")
	_, tok, _ := tokenSvc.Create(user.ID, "To Revoke", "", `["read"]`, "", nil, "", nil)

	if err := tokenSvc.Revoke(tok.ID, user.ID, "no longer needed"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err := tokenSvc.GetByID(tok.ID)
	if err != nil {
		t.Fatalf("GetByID after revoke: %v", err)
	}
	if !revoked.RevokedAt.Valid {
		t.Error("expected RevokedAt to be set")
	}
	if !revoked.RevokedBy.Valid || revoked.RevokedBy.Int64 != user.ID {
		t.Errorf("RevokedBy = %v, want %d", revoked.RevokedBy, user.ID)
	}
	if !revoked.RevokeReason.Valid || revoked.RevokeReason.String != "no longer needed" {
		t.Errorf("RevokeReason = %v, want 'no longer needed'", revoked.RevokeReason)
	}
}

func TestTokenRevoke_AlreadyRevoked(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "double@example.com", "Double", "Revoke")
	_, tok, _ := tokenSvc.Create(user.ID, "Double Revoke", "", `["read"]`, "", nil, "", nil)

	tokenSvc.Revoke(tok.ID, user.ID, "first time")

	err := tokenSvc.Revoke(tok.ID, user.ID, "second time")
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound for already revoked, got %v", err)
	}
}

func TestTokenRevoke_NotFound(t *testing.T) {
	db := setupTestDB(t)
	tokenSvc := NewTokenService(db)

	err := tokenSvc.Revoke(99999, 1, "nonexistent")
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// BulkRevoke
// ---------------------------------------------------------------------------

func TestTokenBulkRevoke(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "bulk@example.com", "Bulk", "User")

	_, tok1, _ := tokenSvc.Create(user.ID, "Bulk 1", "", `["read"]`, "", nil, "", nil)
	_, tok2, _ := tokenSvc.Create(user.ID, "Bulk 2", "", `["read"]`, "", nil, "", nil)
	_, tok3, _ := tokenSvc.Create(user.ID, "Bulk 3 (keep)", "", `["read"]`, "", nil, "", nil)

	revoked, err := tokenSvc.BulkRevoke([]int64{tok1.ID, tok2.ID}, user.ID, "bulk cleanup")
	if err != nil {
		t.Fatalf("BulkRevoke: %v", err)
	}
	if revoked != 2 {
		t.Errorf("revoked = %d, want 2", revoked)
	}

	// Verify tok1 and tok2 are revoked
	r1, _ := tokenSvc.GetByID(tok1.ID)
	r2, _ := tokenSvc.GetByID(tok2.ID)
	if !r1.RevokedAt.Valid {
		t.Error("tok1 should be revoked")
	}
	if !r2.RevokedAt.Valid {
		t.Error("tok2 should be revoked")
	}

	// tok3 should still be active
	r3, _ := tokenSvc.GetByID(tok3.ID)
	if r3.RevokedAt.Valid {
		t.Error("tok3 should not be revoked")
	}
}

func TestTokenBulkRevoke_EmptyList(t *testing.T) {
	db := setupTestDB(t)
	tokenSvc := NewTokenService(db)

	revoked, err := tokenSvc.BulkRevoke([]int64{}, 1, "empty")
	if err != nil {
		t.Fatalf("BulkRevoke(empty): %v", err)
	}
	if revoked != 0 {
		t.Errorf("revoked = %d, want 0", revoked)
	}
}

func TestTokenBulkRevoke_SkipsAlreadyRevoked(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "skiprev@example.com", "Skip", "Rev")

	_, tok1, _ := tokenSvc.Create(user.ID, "Already Revoked", "", `["read"]`, "", nil, "", nil)
	_, tok2, _ := tokenSvc.Create(user.ID, "Still Active", "", `["read"]`, "", nil, "", nil)

	// Pre-revoke tok1
	tokenSvc.Revoke(tok1.ID, user.ID, "pre-revoked")

	revoked, err := tokenSvc.BulkRevoke([]int64{tok1.ID, tok2.ID}, user.ID, "bulk")
	if err != nil {
		t.Fatalf("BulkRevoke: %v", err)
	}
	// Only tok2 should be affected (tok1 was already revoked)
	if revoked != 1 {
		t.Errorf("revoked = %d, want 1 (skip already revoked)", revoked)
	}
}

// ---------------------------------------------------------------------------
// RevokeAllByUser
// ---------------------------------------------------------------------------

func TestTokenRevokeAllByUser(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "revokeall@example.com", "Revoke", "All")
	other := createTestUser(t, userSvc, "other@example.com", "Other", "User")

	tokenSvc.Create(user.ID, "User Token 1", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Create(user.ID, "User Token 2", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Create(other.ID, "Other Token", "", `["read"]`, "", nil, "", nil)

	revoked, err := tokenSvc.RevokeAllByUser(user.ID, user.ID, "account deactivated")
	if err != nil {
		t.Fatalf("RevokeAllByUser: %v", err)
	}
	if revoked != 2 {
		t.Errorf("revoked = %d, want 2", revoked)
	}

	// Other user's token should be untouched
	otherTokens, _ := tokenSvc.ListByUser(other.ID)
	if len(otherTokens) != 1 || otherTokens[0].RevokedAt.Valid {
		t.Error("other user's token should not be revoked")
	}
}

// ---------------------------------------------------------------------------
// RecordUsage
// ---------------------------------------------------------------------------

func TestTokenRecordUsage(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "usage@example.com", "Usage", "User")
	_, tok, _ := tokenSvc.Create(user.ID, "Usage Token", "", `["read"]`, "", nil, "", nil)

	// Initially zero
	if tok.UsageCount != 0 {
		t.Errorf("initial UsageCount = %d, want 0", tok.UsageCount)
	}
	if tok.LastUsedAt.Valid {
		t.Error("expected LastUsedAt to be NULL initially")
	}

	// Record usage 3 times
	tokenSvc.RecordUsage(tok.ID)
	tokenSvc.RecordUsage(tok.ID)
	tokenSvc.RecordUsage(tok.ID)

	updated, err := tokenSvc.GetByID(tok.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if updated.UsageCount != 3 {
		t.Errorf("UsageCount = %d, want 3", updated.UsageCount)
	}
	if !updated.LastUsedAt.Valid {
		t.Error("expected LastUsedAt to be set after RecordUsage")
	}
}

// ---------------------------------------------------------------------------
// ListAll
// ---------------------------------------------------------------------------

func TestTokenListAll(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "listall@example.com", "List", "All")

	tokenSvc.Create(user.ID, "Token A", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Create(user.ID, "Token B", "", `["read"]`, "", nil, "", nil)
	tokenSvc.Create(user.ID, "Token C", "", `["read"]`, "", nil, "", nil)

	tokens, total, err := tokenSvc.ListAll(10, 0)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(tokens) != 3 {
		t.Errorf("len = %d, want 3", len(tokens))
	}
}

func TestTokenListAll_Pagination(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "page@example.com", "Page", "User")

	for i := 0; i < 5; i++ {
		tokenSvc.Create(user.ID, "Token", "", `["read"]`, "", nil, "", nil)
	}

	tokens, total, err := tokenSvc.ListAll(2, 0)
	if err != nil {
		t.Fatalf("ListAll page 1: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(tokens) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(tokens))
	}

	tokens2, _, err := tokenSvc.ListAll(2, 2)
	if err != nil {
		t.Fatalf("ListAll page 2: %v", err)
	}
	if len(tokens2) != 2 {
		t.Errorf("page 2 len = %d, want 2", len(tokens2))
	}
}

// ---------------------------------------------------------------------------
// Unique secrets per create
// ---------------------------------------------------------------------------

func TestTokenCreate_UniqueSecrets(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	tokenSvc := NewTokenService(db)

	user := createTestUser(t, userSvc, "unique@example.com", "Unique", "User")

	secrets := make(map[string]bool)
	for i := 0; i < 10; i++ {
		secret, _, err := tokenSvc.Create(user.ID, "Token", "", `["read"]`, "", nil, "", nil)
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		if secrets[secret] {
			t.Fatalf("duplicate secret on iteration %d", i)
		}
		secrets[secret] = true
	}
}
