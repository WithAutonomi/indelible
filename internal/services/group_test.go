package services

import (
	"testing"
)

func TestGroupCreate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	g, err := svc.Create("engineering", "Eng team", "write")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.Name != "engineering" {
		t.Errorf("Name = %q", g.Name)
	}
	if g.PermissionLevel != "write" {
		t.Errorf("PermissionLevel = %q", g.PermissionLevel)
	}
	if !g.IsActive {
		t.Error("new group should be active")
	}
}

func TestGroupCreate_DuplicateName(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	svc.Create("engineering", "Eng team", "write")
	_, err := svc.Create("engineering", "Duplicate", "read")
	if err != ErrGroupNameTaken {
		t.Errorf("expected ErrGroupNameTaken, got %v", err)
	}
}

func TestGroupGetByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	created, _ := svc.Create("eng", "desc", "write")
	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "eng" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestGroupGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	_, err := svc.GetByID(999)
	if err != ErrGroupNotFound {
		t.Errorf("expected ErrGroupNotFound, got %v", err)
	}
}

func TestGroupList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	svc.Create("alpha", "", "read")
	svc.Create("beta", "", "write")

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 groups, got %d", len(list))
	}
	// Ordered by name
	if list[0].Name != "alpha" {
		t.Errorf("first group = %q, want alpha", list[0].Name)
	}
}

func TestGroupUpdate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	g, _ := svc.Create("eng", "description", "write")

	falseVal := false
	if err := svc.Update(g.ID, "engineering", "new desc", "admin", &falseVal); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := svc.GetByID(g.ID)
	if got.Name != "engineering" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Description != "new desc" {
		t.Errorf("Description = %q", got.Description)
	}
	if got.PermissionLevel != "admin" {
		t.Errorf("PermissionLevel = %q", got.PermissionLevel)
	}
	if got.IsActive {
		t.Error("should be inactive after update")
	}
}

func TestGroupUpdate_PartialFields(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	g, _ := svc.Create("eng", "original desc", "write")

	// Update only name, leave others unchanged
	if err := svc.Update(g.ID, "engineering", "", "", nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := svc.GetByID(g.ID)
	if got.Name != "engineering" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Description != "original desc" {
		t.Errorf("Description should be unchanged, got %q", got.Description)
	}
	if got.PermissionLevel != "write" {
		t.Errorf("PermissionLevel should be unchanged, got %q", got.PermissionLevel)
	}
}

func TestGroupDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)
	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")

	g, _ := svc.Create("eng", "", "write")
	svc.AddMember(g.ID, user.ID, user.ID)

	if err := svc.Delete(g.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := svc.GetByID(g.ID)
	if err != ErrGroupNotFound {
		t.Errorf("expected ErrGroupNotFound, got %v", err)
	}

	// Members should be cleaned up
	members, _ := svc.ListMembers(g.ID)
	if len(members) != 0 {
		t.Errorf("expected 0 members after delete, got %d", len(members))
	}
}

func TestGroupAddMember(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)

	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")
	g, _ := svc.Create("eng", "", "write")

	if err := svc.AddMember(g.ID, user.ID, user.ID); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	members, _ := svc.ListMembers(g.ID)
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0] != user.ID {
		t.Errorf("member ID = %d, want %d", members[0], user.ID)
	}
}

func TestGroupAddMember_Duplicate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)

	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")
	g, _ := svc.Create("eng", "", "write")

	svc.AddMember(g.ID, user.ID, user.ID)
	err := svc.AddMember(g.ID, user.ID, user.ID)
	if err != ErrAlreadyMember {
		t.Errorf("expected ErrAlreadyMember, got %v", err)
	}
}

func TestGroupRemoveMember(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)

	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")
	g, _ := svc.Create("eng", "", "write")
	svc.AddMember(g.ID, user.ID, user.ID)

	if err := svc.RemoveMember(g.ID, user.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members, _ := svc.ListMembers(g.ID)
	if len(members) != 0 {
		t.Errorf("expected 0 members, got %d", len(members))
	}
}

func TestGroupRemoveMember_NotMember(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)

	user := createTestUser(t, userSvc, "test@test.com", "Test", "User")
	g, _ := svc.Create("eng", "", "write")

	err := svc.RemoveMember(g.ID, user.ID)
	if err != ErrNotMember {
		t.Errorf("expected ErrNotMember, got %v", err)
	}
}

func TestGroupMemberCount(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)

	u1 := createTestUser(t, userSvc, "a@test.com", "A", "User")
	u2 := createTestUser(t, userSvc, "b@test.com", "B", "User")
	g, _ := svc.Create("eng", "", "write")

	svc.AddMember(g.ID, u1.ID, u1.ID)
	svc.AddMember(g.ID, u2.ID, u1.ID)

	count, err := svc.MemberCount(g.ID)
	if err != nil {
		t.Fatalf("MemberCount: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestGroupReplaceMembers(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)
	userSvc := NewUserService(db)

	u1 := createTestUser(t, userSvc, "a@test.com", "A", "User")
	u2 := createTestUser(t, userSvc, "b@test.com", "B", "User")
	u3 := createTestUser(t, userSvc, "c@test.com", "C", "User")
	g, _ := svc.Create("eng", "", "write")

	svc.AddMember(g.ID, u1.ID, u1.ID)
	svc.AddMember(g.ID, u2.ID, u1.ID)

	// Replace with u2 and u3
	if err := svc.ReplaceMembers(g.ID, []int64{u2.ID, u3.ID}, u1.ID); err != nil {
		t.Fatalf("ReplaceMembers: %v", err)
	}

	members, _ := svc.ListMembers(g.ID)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestGroupExternalID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewGroupService(db)

	g, _ := svc.Create("eng", "", "write")

	if err := svc.SetExternalID(g.ID, "scim-group-123"); err != nil {
		t.Fatalf("SetExternalID: %v", err)
	}

	got, err := svc.GetByExternalID("scim-group-123")
	if err != nil {
		t.Fatalf("GetByExternalID: %v", err)
	}
	if got.ID != g.ID {
		t.Errorf("got ID=%d, want %d", got.ID, g.ID)
	}
}
