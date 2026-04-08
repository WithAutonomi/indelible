package services

import "database/sql"

// CollectionTagService manages tags on collections and their inheritance to files.
type CollectionTagService struct {
	db *sql.DB
}

// NewCollectionTagService creates a new CollectionTagService.
func NewCollectionTagService(db *sql.DB) *CollectionTagService {
	return &CollectionTagService{db: db}
}

// SetTags replaces all tags on a collection (upsert semantics).
func (s *CollectionTagService) SetTags(collectionID int64, tags map[string][]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM collection_tags WHERE collection_id = ?`, collectionID); err != nil {
		return err
	}
	for k, vals := range tags {
		for _, v := range vals {
			_, err := tx.Exec(
				`INSERT INTO collection_tags (collection_id, tag_key, tag_value) VALUES (?, ?, ?)`,
				collectionID, k, v,
			)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GetTags returns all tags on a collection.
func (s *CollectionTagService) GetTags(collectionID int64) (map[string][]string, error) {
	rows, err := s.db.Query(
		`SELECT tag_key, tag_value FROM collection_tags WHERE collection_id = ? ORDER BY tag_key, id`, collectionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make(map[string][]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		tags[k] = append(tags[k], v)
	}
	return tags, rows.Err()
}

// InheritToFile copies collection tags to a file upload, without overwriting existing file tags.
// Returns the number of tags added.
func (s *CollectionTagService) InheritToFile(collectionID, uploadID int64) (int, error) {
	// Get collection tags
	collTags, err := s.GetTags(collectionID)
	if err != nil || len(collTags) == 0 {
		return 0, err
	}

	// Get existing file tag keys to avoid overwriting
	rows, err := s.db.Query(`SELECT DISTINCT tag_key FROM file_tags WHERE upload_id = ?`, uploadID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	existing := make(map[string]struct{})
	for rows.Next() {
		var k string
		rows.Scan(&k)
		existing[k] = struct{}{}
	}

	// Insert collection tags that don't conflict with existing file tag keys
	added := 0
	for k, vals := range collTags {
		if _, exists := existing[k]; exists {
			continue
		}
		for _, v := range vals {
			_, err := s.db.Exec(
				`INSERT INTO file_tags (upload_id, tag_key, tag_value) VALUES (?, ?, ?)`,
				uploadID, k, v,
			)
			if err == nil {
				added++
			}
		}
	}
	return added, nil
}
