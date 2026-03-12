package services

import (
	"database/sql"
	"errors"
)

var ErrLastAdmin = errors.New("cannot remove the last admin")

// PermissionService handles permission-related database operations.
type PermissionService struct {
	db *sql.DB
}

func NewPermissionService(db *sql.DB) *PermissionService {
	return &PermissionService{db: db}
}

// SetDirect sets or updates a user's direct permission level.
func (s *PermissionService) SetDirect(userID int64, level string, grantedBy int64) error {
	_, err := s.db.Exec(
		`INSERT INTO user_permissions (user_id, permission_level, granted_by)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET permission_level = ?, granted_by = ?, created_at = CURRENT_TIMESTAMP`,
		userID, level, grantedBy, level, grantedBy,
	)
	return err
}

// GetDirect returns a user's direct permission level, or empty string if none.
func (s *PermissionService) GetDirect(userID int64) (string, error) {
	var level string
	err := s.db.QueryRow(
		`SELECT permission_level FROM user_permissions WHERE user_id = ?`,
		userID,
	).Scan(&level)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return level, err
}

// GetEffective returns the highest permission level a user has,
// combining direct permissions and group-inherited permissions.
// Hierarchy: admin > write > read > "" (none)
func (s *PermissionService) GetEffective(userID int64) (string, error) {
	// Get direct permission
	direct, err := s.GetDirect(userID)
	if err != nil {
		return "", err
	}

	// Get highest group permission
	var groupLevel sql.NullString
	err = s.db.QueryRow(
		`SELECT g.permission_level FROM groups g
		 JOIN group_members gm ON gm.group_id = g.id
		 WHERE gm.user_id = ? AND g.is_active = 1
		 ORDER BY CASE g.permission_level
		   WHEN 'admin' THEN 3
		   WHEN 'write' THEN 2
		   WHEN 'read' THEN 1
		   ELSE 0 END DESC
		 LIMIT 1`,
		userID,
	).Scan(&groupLevel)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	group := ""
	if groupLevel.Valid {
		group = groupLevel.String
	}

	return highest(direct, group), nil
}

// IsAdmin checks if a user has admin-level effective permissions.
func (s *PermissionService) IsAdmin(userID int64) (bool, error) {
	level, err := s.GetEffective(userID)
	if err != nil {
		return false, err
	}
	return level == "admin", nil
}

// CountAdmins returns the number of active users with admin permissions
// (direct or group-inherited).
func (s *PermissionService) CountAdmins() (int64, error) {
	var count int64
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT u.id) FROM users u
		 LEFT JOIN user_permissions up ON up.user_id = u.id
		 LEFT JOIN group_members gm ON gm.user_id = u.id
		 LEFT JOIN groups g ON g.id = gm.group_id AND g.is_active = 1
		 WHERE u.is_active = 1 AND u.deleted_at IS NULL
		   AND (up.permission_level = 'admin' OR g.permission_level = 'admin')`,
	).Scan(&count)
	return count, err
}

// highest returns the higher of two permission levels.
func highest(a, b string) string {
	rank := map[string]int{"": 0, "read": 1, "write": 2, "admin": 3}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}
