package services

import (
	"database/sql"
	"time"
)

// Tag represents a key-value tag on an upload.
type Tag struct {
	ID        int64
	UploadID  int64
	Key       string
	Value     string
	CreatedAt time.Time
}

// TagService handles file tag operations.
type TagService struct {
	db *sql.DB
}

// NewTagService creates a new TagService.
func NewTagService(db *sql.DB) *TagService {
	return &TagService{db: db}
}

// SetTags replaces all tags on an upload with the given key-value pairs.
// Uses upsert semantics: existing keys are updated, new keys are inserted, missing keys are deleted.
func (s *TagService) SetTags(uploadID int64, tags map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete all existing tags for this upload
	if _, err := tx.Exec(`DELETE FROM file_tags WHERE upload_id = ?`, uploadID); err != nil {
		return err
	}

	// Insert new tags
	for k, v := range tags {
		if _, err := tx.Exec(
			`INSERT INTO file_tags (upload_id, tag_key, tag_value) VALUES (?, ?, ?)`,
			uploadID, k, v,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetTags returns all tags for an upload as a key-value map.
func (s *TagService) GetTags(uploadID int64) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT tag_key, tag_value FROM file_tags WHERE upload_id = ?`, uploadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		tags[k] = v
	}
	return tags, rows.Err()
}

// SearchResult holds a search result with upload info and matching tags.
type SearchResult struct {
	Upload *Upload
	Tags   map[string]string
}

// ListKeys returns all distinct tag keys used by a user's uploads.
func (s *TagService) ListKeys(userID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT ft.tag_key FROM file_tags ft
		 INNER JOIN uploads u ON ft.upload_id = u.id
		 WHERE u.user_id = ?
		 ORDER BY ft.tag_key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListValues returns all distinct values for a given tag key used by a user's uploads.
func (s *TagService) ListValues(userID int64, key string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT ft.tag_value FROM file_tags ft
		 INNER JOIN uploads u ON ft.upload_id = u.id
		 WHERE u.user_id = ? AND ft.tag_key = ?
		 ORDER BY ft.tag_value`, userID, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// Search finds uploads matching tag filters and/or filename query.
// tagFilters: map of tag_key -> tag_value (all must match)
// query: substring match against filename (empty = no filename filter)
// userID: filter to specific user (0 = all users, for admin)
func (s *TagService) Search(tagFilters map[string]string, query string, userID int64, limit, offset int) ([]*SearchResult, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	// Build the query dynamically
	baseQuery := `FROM uploads u`
	where := ` WHERE 1=1`
	args := []any{}

	// Join for each tag filter
	i := 0
	for k, v := range tagFilters {
		alias := "t" + string(rune('0'+i))
		baseQuery += ` INNER JOIN file_tags ` + alias + ` ON u.id = ` + alias + `.upload_id AND ` + alias + `.tag_key = ? AND ` + alias + `.tag_value = ?`
		args = append(args, k, v)
		i++
	}

	if query != "" {
		where += ` AND (u.original_filename LIKE ? OR u.filename LIKE ?)`
		wildcard := "%" + query + "%"
		args = append(args, wildcard, wildcard)
	}

	if userID > 0 {
		where += ` AND u.user_id = ?`
		args = append(args, userID)
	}

	// Count total
	var total int64
	countSQL := `SELECT COUNT(DISTINCT u.id) ` + baseQuery + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Fetch results
	selectSQL := `SELECT DISTINCT u.id, u.uuid, u.user_id, u.token_id, u.filename, u.original_filename, u.file_size, u.content_type, u.visibility, u.status,
		u.status_detail, u.datamap_address, u.estimated_cost, u.actual_cost, u.error_message, u.temp_path,
		u.data_map, u.backoff_until, u.backoff_attempt, u.last_quoted_cost,
		u.queued_at, u.processing_at, u.completed_at, u.failed_at, u.created_at ` +
		baseQuery + where + ` ORDER BY u.created_at DESC LIMIT ? OFFSET ?`
	queryArgs := append(args, limit, offset)

	rows, err := s.db.Query(selectSQL, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, 0, err
		}

		tags, _ := s.GetTags(u.ID)
		results = append(results, &SearchResult{Upload: u, Tags: tags})

	}

	return results, total, rows.Err()
}

// SearchBySelector finds uploads matching pre-built SQL selector clauses.
// Used by bulk tag operations and the enhanced search endpoint.
func (s *TagService) SearchBySelector(userID int64, selectorClauses []string, selectorArgs []interface{}, limit int) ([]*Upload, error) {
	if limit <= 0 || limit > 10000 {
		limit = 100
	}

	query := `SELECT DISTINCT u.id, u.uuid, u.user_id, u.token_id, u.filename, u.original_filename, u.file_size, u.content_type, u.visibility, u.status,
		u.status_detail, u.datamap_address, u.estimated_cost, u.actual_cost, u.error_message, u.temp_path,
		u.data_map, u.backoff_until, u.backoff_attempt, u.last_quoted_cost,
		u.queued_at, u.processing_at, u.completed_at, u.failed_at, u.created_at
		FROM uploads u WHERE u.user_id = ?`
	args := []interface{}{userID}

	for _, clause := range selectorClauses {
		query += " AND " + clause
	}
	args = append(args, selectorArgs...)
	query += " ORDER BY u.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var uploads []*Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, u)
	}
	return uploads, rows.Err()
}

// SearchWithSelector performs a full search using label selectors, with pagination and tags included.
func (s *TagService) SearchWithSelector(selectorClauses []string, selectorArgs []interface{}, query string, userID int64, limit, offset int) ([]*SearchResult, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	baseSQL := `FROM uploads u WHERE 1=1`
	args := []interface{}{}

	for _, clause := range selectorClauses {
		baseSQL += " AND " + clause
	}
	args = append(args, selectorArgs...)

	if query != "" {
		baseSQL += ` AND (u.original_filename LIKE ? OR u.filename LIKE ?)`
		wildcard := "%" + query + "%"
		args = append(args, wildcard, wildcard)
	}

	if userID > 0 {
		baseSQL += ` AND u.user_id = ?`
		args = append(args, userID)
	}

	// Count
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT u.id) `+baseSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Fetch
	selectSQL := `SELECT DISTINCT u.id, u.uuid, u.user_id, u.token_id, u.filename, u.original_filename, u.file_size, u.content_type, u.visibility, u.status,
		u.status_detail, u.datamap_address, u.estimated_cost, u.actual_cost, u.error_message, u.temp_path,
		u.data_map, u.backoff_until, u.backoff_attempt, u.last_quoted_cost,
		u.queued_at, u.processing_at, u.completed_at, u.failed_at, u.created_at ` +
		baseSQL + ` ORDER BY u.created_at DESC LIMIT ? OFFSET ?`
	queryArgs := append(args, limit, offset)

	rows, err := s.db.Query(selectSQL, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, 0, err
		}
		tags, _ := s.GetTags(u.ID)
		results = append(results, &SearchResult{Upload: u, Tags: tags})
	}

	return results, total, rows.Err()
}
