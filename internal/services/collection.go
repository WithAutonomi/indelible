package services

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrCollectionNotFound = errors.New("collection not found")
	ErrCollectionNameTaken = errors.New("collection name already exists at this level")
	ErrFileAlreadyInCollection = errors.New("file already in collection")
	ErrFileNotInCollection = errors.New("file not in collection")
)

// Collection represents a virtual folder.
type Collection struct {
	ID          int64
	Name        string
	Description string
	ParentID    sql.NullInt64
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	FileCount   int64 // populated by List/Get queries
}

// CollectionService handles collection operations.
type CollectionService struct {
	db *sql.DB
}

// NewCollectionService creates a new CollectionService.
func NewCollectionService(db *sql.DB) *CollectionService {
	return &CollectionService{db: db}
}

// Create adds a new collection.
func (s *CollectionService) Create(name, description string, parentID *int64, createdBy int64) (*Collection, error) {
	var pID sql.NullInt64
	if parentID != nil {
		// Verify parent exists
		var exists bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM collections WHERE id = ?)`, *parentID).Scan(&exists)
		if !exists {
			return nil, ErrCollectionNotFound
		}
		pID = sql.NullInt64{Int64: *parentID, Valid: true}
	}

	var id int64
	err := s.db.QueryRow(
		`INSERT INTO collections (name, description, parent_id, created_by) VALUES (?, ?, ?, ?) RETURNING id`,
		name, description, pID, createdBy,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCollectionNameTaken
		}
		return nil, err
	}
	return s.GetByID(id)
}

// GetByID retrieves a collection with its file count.
func (s *CollectionService) GetByID(id int64) (*Collection, error) {
	c := &Collection{}
	err := s.db.QueryRow(
		`SELECT c.id, c.name, c.description, c.parent_id, c.created_by, c.created_at, c.updated_at,
		        (SELECT COUNT(*) FROM collection_files cf WHERE cf.collection_id = c.id) as file_count
		 FROM collections c WHERE c.id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Description, &c.ParentID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.FileCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCollectionNotFound
		}
		return nil, err
	}
	return c, nil
}

// List returns collections for a user. If parentID is nil, returns top-level collections.
func (s *CollectionService) List(userID int64, parentID *int64) ([]*Collection, error) {
	var rows *sql.Rows
	var err error

	if parentID != nil {
		rows, err = s.db.Query(
			`SELECT c.id, c.name, c.description, c.parent_id, c.created_by, c.created_at, c.updated_at,
			        (SELECT COUNT(*) FROM collection_files cf WHERE cf.collection_id = c.id) as file_count
			 FROM collections c WHERE c.created_by = ? AND c.parent_id = ? ORDER BY c.name ASC`,
			userID, *parentID,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT c.id, c.name, c.description, c.parent_id, c.created_by, c.created_at, c.updated_at,
			        (SELECT COUNT(*) FROM collection_files cf WHERE cf.collection_id = c.id) as file_count
			 FROM collections c WHERE c.created_by = ? AND c.parent_id IS NULL ORDER BY c.name ASC`,
			userID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []*Collection
	for rows.Next() {
		c := &Collection{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.ParentID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.FileCount); err != nil {
			return nil, err
		}
		collections = append(collections, c)
	}
	return collections, rows.Err()
}

// Update modifies a collection's name and description.
func (s *CollectionService) Update(id int64, name, description string) (*Collection, error) {
	_, err := s.db.Exec(
		`UPDATE collections SET name = ?, description = ?, updated_at = datetime('now') WHERE id = ?`,
		name, description, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

// Delete removes a collection and its file associations. Does not delete the files themselves.
// Also deletes child collections recursively.
func (s *CollectionService) Delete(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteCollectionRecursive(tx, id); err != nil {
		return err
	}

	return tx.Commit()
}

func deleteCollectionRecursive(tx *sql.Tx, id int64) error {
	// Find children
	rows, err := tx.Query(`SELECT id FROM collections WHERE parent_id = ?`, id)
	if err != nil {
		return err
	}
	var childIDs []int64
	for rows.Next() {
		var childID int64
		if err := rows.Scan(&childID); err != nil {
			rows.Close()
			return err
		}
		childIDs = append(childIDs, childID)
	}
	rows.Close()

	// Delete children first
	for _, childID := range childIDs {
		if err := deleteCollectionRecursive(tx, childID); err != nil {
			return err
		}
	}

	// Delete file associations
	if _, err := tx.Exec(`DELETE FROM collection_files WHERE collection_id = ?`, id); err != nil {
		return err
	}

	// Delete the collection itself
	_, err = tx.Exec(`DELETE FROM collections WHERE id = ?`, id)
	return err
}

// AddFile adds an upload to a collection.
func (s *CollectionService) AddFile(collectionID, uploadID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO collection_files (collection_id, upload_id) VALUES (?, ?)`,
		collectionID, uploadID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrFileAlreadyInCollection
		}
		return err
	}
	return nil
}

// CollectionIDsForUpload returns the IDs of collections containing a given upload.
func (s *CollectionService) CollectionIDsForUpload(uploadID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT collection_id FROM collection_files WHERE upload_id = ?`, uploadID,
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

// RemoveFile removes an upload from a collection.
func (s *CollectionService) RemoveFile(collectionID, uploadID int64) error {
	result, err := s.db.Exec(
		`DELETE FROM collection_files WHERE collection_id = ? AND upload_id = ?`,
		collectionID, uploadID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrFileNotInCollection
	}
	return nil
}

// ListFiles returns uploads in a collection.
func (s *CollectionService) ListFiles(collectionID int64, limit, offset int) ([]*Upload, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM collection_files WHERE collection_id = ?`, collectionID).Scan(&total)

	rows, err := s.db.Query(
		`SELECT u.id, u.uuid, u.user_id, u.token_id, u.filename, u.original_filename, u.file_size, u.content_type, u.visibility, u.status,
		        u.status_detail, u.datamap_address, u.estimated_cost, u.actual_cost, u.error_message, u.temp_path,
		        u.data_map, u.backoff_until, u.backoff_attempt, u.last_quoted_cost,
		        u.queued_at, u.processing_at, u.completed_at, u.failed_at, u.created_at
		 FROM uploads u
		 INNER JOIN collection_files cf ON u.id = cf.upload_id
		 WHERE cf.collection_id = ?
		 ORDER BY cf.added_at DESC LIMIT ? OFFSET ?`,
		collectionID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanUploads(rows, total)
}
