package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrGroupNotFound  = errors.New("group not found")
	ErrGroupNameTaken = errors.New("group name already exists")
	ErrAlreadyMember  = errors.New("user is already a member")
	ErrNotMember      = errors.New("user is not a member")
)

type Group struct {
	ID              int64
	Name            string
	Description     string
	PermissionLevel string
	IsActive        bool
	ExternalID      sql.NullString
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type GroupMember struct {
	ID        int64
	GroupID   int64
	UserID    int64
	AddedBy   sql.NullInt64
	CreatedAt time.Time
}

type GroupService struct {
	db *database.DB
}

func NewGroupService(db *database.DB) *GroupService {
	return &GroupService{db: db}
}

func (s *GroupService) Create(name, description, permissionLevel string) (*Group, error) {
	var id int64
	err := s.db.QueryRow(
		`INSERT INTO groups (name, description, permission_level)
		 VALUES (?, ?, ?)
		 RETURNING id`,
		name, description, permissionLevel,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrGroupNameTaken
		}
		return nil, err
	}
	return s.GetByID(id)
}

func (s *GroupService) GetByID(id int64) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(
		`SELECT id, name, description, permission_level, is_active, external_id, created_at, updated_at
		 FROM groups WHERE id = ?`,
		id,
	).Scan(&g.ID, &g.Name, &g.Description, &g.PermissionLevel, &g.IsActive, &g.ExternalID, &g.CreatedAt, &g.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrGroupNotFound
	}
	return g, err
}

func (s *GroupService) List() ([]*Group, error) {
	rows, err := s.db.Query(
		`SELECT id, name, description, permission_level, is_active, external_id, created_at, updated_at
		 FROM groups ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		g := &Group{}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.PermissionLevel, &g.IsActive, &g.ExternalID, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *GroupService) Update(id int64, name, description, permissionLevel string, isActive *bool) error {
	g, err := s.GetByID(id)
	if err != nil {
		return err
	}
	if name != "" {
		g.Name = name
	}
	if description != "" {
		g.Description = description
	}
	if permissionLevel != "" {
		g.PermissionLevel = permissionLevel
	}
	active := g.IsActive
	if isActive != nil {
		active = *isActive
	}

	_, err = s.db.Exec(
		`UPDATE groups SET name = ?, description = ?, permission_level = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		g.Name, g.Description, g.PermissionLevel, active, id,
	)
	return err
}

func (s *GroupService) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM group_members WHERE group_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	return err
}

func (s *GroupService) AddMember(groupID, userID, addedBy int64) error {
	_, err := s.db.Exec(
		`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
		groupID, userID, nullableAddedBy(addedBy),
	)
	if err != nil && isUniqueViolation(err) {
		return ErrAlreadyMember
	}
	return err
}

// nullableAddedBy translates the addedBy=0 sentinel used by SCIM provisioning
// (where there is no acting user) into a SQL NULL, so the optional FK to
// users(id) doesn't fail with a constraint violation.
func nullableAddedBy(addedBy int64) sql.NullInt64 {
	if addedBy == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: addedBy, Valid: true}
}

func (s *GroupService) RemoveMember(groupID, userID int64) error {
	result, err := s.db.Exec(
		`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`,
		groupID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotMember
	}
	return nil
}

func (s *GroupService) ListMembers(groupID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT user_id FROM group_members WHERE group_id = ?`, groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MemberCount returns the number of members in a group.
func (s *GroupService) MemberCount(groupID int64) (int64, error) {
	var count int64
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID,
	).Scan(&count)
	return count, err
}

// GetByExternalID retrieves a group by its SCIM external ID.
func (s *GroupService) GetByExternalID(externalID string) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(
		`SELECT id, name, description, permission_level, is_active, external_id, created_at, updated_at
		 FROM groups WHERE external_id = ?`,
		externalID,
	).Scan(&g.ID, &g.Name, &g.Description, &g.PermissionLevel, &g.IsActive, &g.ExternalID, &g.CreatedAt, &g.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrGroupNotFound
	}
	return g, err
}

// SetExternalID sets the SCIM external ID for a group.
func (s *GroupService) SetExternalID(id int64, externalID string) error {
	_, err := s.db.Exec(
		`UPDATE groups SET external_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		externalID, id,
	)
	return err
}

// ReplaceMembers atomically replaces all group members.
func (s *GroupService) ReplaceMembers(groupID int64, userIDs []int64, addedBy int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM group_members WHERE group_id = ?`, groupID); err != nil {
		return err
	}

	added := nullableAddedBy(addedBy)
	for _, uid := range userIDs {
		if _, err := tx.Exec(
			`INSERT INTO group_members (group_id, user_id, added_by) VALUES (?, ?, ?)`,
			groupID, uid, added,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}
