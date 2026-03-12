package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUploadNotFound = errors.New("upload not found")
)

// Upload represents a file upload record.
type Upload struct {
	ID               int64
	UUID             string
	UserID           int64
	TokenID          sql.NullInt64
	Filename         string
	OriginalFilename string
	FileSize         int64
	ContentType      string
	Visibility       string // "public" or "private"
	Status           string // "queued", "processing", "completed", "failed"
	DatamapAddress   sql.NullString
	EstimatedCost    sql.NullString
	ActualCost       sql.NullString
	ErrorMessage     sql.NullString
	TempPath         sql.NullString
	QueuedAt         time.Time
	ProcessingAt     sql.NullTime
	CompletedAt      sql.NullTime
	FailedAt         sql.NullTime
	CreatedAt        time.Time
}

// UploadService handles database operations for file uploads.
type UploadService struct {
	db *sql.DB
}

// NewUploadService creates a new UploadService.
func NewUploadService(db *sql.DB) *UploadService {
	return &UploadService{db: db}
}

// Create inserts a new upload record with status "queued".
func (s *UploadService) Create(userID int64, tokenID *int64, filename, originalFilename string, fileSize int64, contentType, visibility, tempPath string, estimatedCost *string) (*Upload, error) {
	uid := uuid.New().String()

	var tID sql.NullInt64
	if tokenID != nil {
		tID = sql.NullInt64{Int64: *tokenID, Valid: true}
	}
	var estCost sql.NullString
	if estimatedCost != nil {
		estCost = sql.NullString{String: *estimatedCost, Valid: true}
	}

	result, err := s.db.Exec(
		`INSERT INTO uploads (uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status, estimated_cost, temp_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?)`,
		uid, userID, tID, filename, originalFilename, fileSize, contentType, visibility, estCost, tempPath,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return s.GetByID(id)
}

// GetByID retrieves an upload by internal ID.
func (s *UploadService) GetByID(id int64) (*Upload, error) {
	u := &Upload{}
	err := s.db.QueryRow(
		`SELECT id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
		        datamap_address, estimated_cost, actual_cost, error_message, temp_path,
		        queued_at, processing_at, completed_at, failed_at, created_at
		 FROM uploads WHERE id = ?`, id,
	).Scan(
		&u.ID, &u.UUID, &u.UserID, &u.TokenID, &u.Filename, &u.OriginalFilename, &u.FileSize, &u.ContentType,
		&u.Visibility, &u.Status, &u.DatamapAddress, &u.EstimatedCost, &u.ActualCost, &u.ErrorMessage, &u.TempPath,
		&u.QueuedAt, &u.ProcessingAt, &u.CompletedAt, &u.FailedAt, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUploadNotFound
		}
		return nil, err
	}
	return u, nil
}

// GetByUUID retrieves an upload by public UUID.
func (s *UploadService) GetByUUID(uid string) (*Upload, error) {
	u := &Upload{}
	err := s.db.QueryRow(
		`SELECT id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
		        datamap_address, estimated_cost, actual_cost, error_message, temp_path,
		        queued_at, processing_at, completed_at, failed_at, created_at
		 FROM uploads WHERE uuid = ?`, uid,
	).Scan(
		&u.ID, &u.UUID, &u.UserID, &u.TokenID, &u.Filename, &u.OriginalFilename, &u.FileSize, &u.ContentType,
		&u.Visibility, &u.Status, &u.DatamapAddress, &u.EstimatedCost, &u.ActualCost, &u.ErrorMessage, &u.TempPath,
		&u.QueuedAt, &u.ProcessingAt, &u.CompletedAt, &u.FailedAt, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUploadNotFound
		}
		return nil, err
	}
	return u, nil
}

// ListByUser returns uploads for a specific user, newest first.
func (s *UploadService) ListByUser(userID int64, limit, offset int) ([]*Upload, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var total int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM uploads WHERE user_id = ?`, userID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		`SELECT id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
		        datamap_address, estimated_cost, actual_cost, error_message, temp_path,
		        queued_at, processing_at, completed_at, failed_at, created_at
		 FROM uploads WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanUploads(rows, total)
}

// ListAll returns all uploads (admin), newest first.
func (s *UploadService) ListAll(limit, offset int) ([]*Upload, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var total int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM uploads`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		`SELECT id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
		        datamap_address, estimated_cost, actual_cost, error_message, temp_path,
		        queued_at, processing_at, completed_at, failed_at, created_at
		 FROM uploads ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanUploads(rows, total)
}

// DequeueNext atomically claims the next queued upload for processing.
// Returns nil, nil if no uploads are queued.
func (s *UploadService) DequeueNext() (*Upload, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM uploads WHERE status = 'queued' ORDER BY queued_at ASC LIMIT 1`,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	_, err = tx.Exec(
		`UPDATE uploads SET status = 'processing', processing_at = datetime('now') WHERE id = ? AND status = 'queued'`,
		id,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

// MarkCompleted transitions an upload to "completed" with the network address and cost.
func (s *UploadService) MarkCompleted(id int64, datamapAddress, actualCost string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'completed', datamap_address = ?, actual_cost = ?, completed_at = datetime('now'), temp_path = NULL WHERE id = ?`,
		datamapAddress, actualCost, id,
	)
	return err
}

// MarkFailed transitions an upload to "failed" with an error message.
func (s *UploadService) MarkFailed(id int64, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'failed', error_message = ?, failed_at = datetime('now'), temp_path = NULL WHERE id = ?`,
		errMsg, id,
	)
	return err
}

// RequeueStuck finds uploads that have been "processing" for longer than the timeout
// and resets them to "queued". Returns the number of requeued uploads.
func (s *UploadService) RequeueStuck(timeoutMinutes int) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE uploads SET status = 'queued', processing_at = NULL
		 WHERE status = 'processing'
		 AND processing_at < datetime('now', ? || ' minutes')`,
		-timeoutMinutes,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CountByStatus returns the number of uploads in each status.
func (s *UploadService) CountByStatus() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM uploads GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func scanUploads(rows *sql.Rows, total int64) ([]*Upload, int64, error) {
	var uploads []*Upload
	for rows.Next() {
		u := &Upload{}
		if err := rows.Scan(
			&u.ID, &u.UUID, &u.UserID, &u.TokenID, &u.Filename, &u.OriginalFilename, &u.FileSize, &u.ContentType,
			&u.Visibility, &u.Status, &u.DatamapAddress, &u.EstimatedCost, &u.ActualCost, &u.ErrorMessage, &u.TempPath,
			&u.QueuedAt, &u.ProcessingAt, &u.CompletedAt, &u.FailedAt, &u.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		uploads = append(uploads, u)
	}
	return uploads, total, rows.Err()
}
