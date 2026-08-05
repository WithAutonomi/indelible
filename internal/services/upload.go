package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/downloadcache"
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
	Visibility       string         // "public" or "private"
	Status           string         // "queued", "processing", "completed", "failed"
	StatusDetail     sql.NullString // substatus: "gas_backoff", etc.
	DatamapAddress   sql.NullString
	EstimatedCost    sql.NullString
	ActualCost       sql.NullString
	ErrorMessage     sql.NullString
	TempPath         sql.NullString
	DataMap          sql.NullString // hex-encoded serialized DataMap (stored locally, not on-network)
	BackoffUntil     sql.NullTime
	BackoffAttempt   int
	LastQuotedCost   sql.NullString
	QueuedAt         time.Time
	ProcessingAt     sql.NullTime
	CompletedAt      sql.NullTime
	FailedAt         sql.NullTime
	CreatedAt        time.Time
}

const uploadColumns = `id, uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status,
	status_detail, datamap_address, estimated_cost, actual_cost, error_message, temp_path,
	data_map, backoff_until, backoff_attempt, last_quoted_cost,
	queued_at, processing_at, completed_at, failed_at, created_at`

func scanUpload(scanner interface{ Scan(...any) error }) (*Upload, error) {
	u := &Upload{}
	err := scanner.Scan(
		&u.ID, &u.UUID, &u.UserID, &u.TokenID, &u.Filename, &u.OriginalFilename, &u.FileSize, &u.ContentType,
		&u.Visibility, &u.Status, &u.StatusDetail, &u.DatamapAddress, &u.EstimatedCost, &u.ActualCost, &u.ErrorMessage, &u.TempPath,
		&u.DataMap, &u.BackoffUntil, &u.BackoffAttempt, &u.LastQuotedCost,
		&u.QueuedAt, &u.ProcessingAt, &u.CompletedAt, &u.FailedAt, &u.CreatedAt,
	)
	return u, err
}

// UploadService handles database operations for file uploads.
type UploadService struct {
	db *database.DB
}

// NewUploadService creates a new UploadService.
func NewUploadService(db *database.DB) *UploadService {
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

	var id int64
	err := s.db.QueryRow(
		`INSERT INTO uploads (uuid, user_id, token_id, filename, original_filename, file_size, content_type, visibility, status, estimated_cost, temp_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?) RETURNING id`,
		uid, userID, tID, filename, originalFilename, fileSize, contentType, visibility, estCost, tempPath,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

// GetByID retrieves an upload by internal ID.
func (s *UploadService) GetByID(id int64) (*Upload, error) {
	u, err := scanUpload(s.db.QueryRow(`SELECT `+uploadColumns+` FROM uploads WHERE id = ?`, id))
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
	u, err := scanUpload(s.db.QueryRow(`SELECT `+uploadColumns+` FROM uploads WHERE uuid = ?`, uid))
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
		`SELECT `+uploadColumns+` FROM uploads WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
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
		`SELECT `+uploadColumns+` FROM uploads ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanUploads(rows, total)
}

// UploadListOptions configures filtered, sorted upload listing.
type UploadListOptions struct {
	UserID    int64
	Limit     int
	Offset    int
	SortBy    string // "created_at" (default), "file_size", "filename", "status"
	SortOrder string // "desc" (default), "asc"
	Status    string // filter: "queued", "processing", "completed", "failed", "" (all)
	From      *time.Time
	To        *time.Time
}

// ListByUserFiltered returns uploads matching filter/sort criteria.
func (s *UploadService) ListByUserFiltered(opts UploadListOptions) ([]*Upload, int64, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 50
	}

	// Whitelist sort columns to prevent injection
	validSorts := map[string]string{
		"created_at": "created_at",
		"file_size":  "file_size",
		"filename":   "original_filename",
		"status":     "status",
	}
	sortCol := "created_at"
	if col, ok := validSorts[opts.SortBy]; ok {
		sortCol = col
	}
	sortDir := "DESC"
	if opts.SortOrder == "asc" {
		sortDir = "ASC"
	}

	// Build WHERE conditions
	conditions := []string{"user_id = ?"}
	args := []any{opts.UserID}

	if opts.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, opts.Status)
	}
	if opts.From != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, opts.From.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if opts.To != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, opts.To.UTC().Format("2006-01-02T15:04:05Z"))
	}

	where := ""
	for i, c := range conditions {
		if i == 0 {
			where = "WHERE " + c
		} else {
			where += " AND " + c
		}
	}

	// Count
	var total int64
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := s.db.QueryRow("SELECT COUNT(*) FROM uploads "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Query
	query := "SELECT " + uploadColumns + " FROM uploads " + where +
		" ORDER BY " + sortCol + " " + sortDir + " LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanUploads(rows, total)
}

// ListByUserCursor returns uploads using cursor-based pagination.
func (s *UploadService) ListByUserCursor(userID int64, limit int, cursorID int64, forward bool) ([]*Upload, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if forward {
		// Next page: IDs less than cursor (descending order)
		rows, err = s.db.Query(
			`SELECT `+uploadColumns+` FROM uploads WHERE user_id = ? AND id < ? ORDER BY id DESC LIMIT ?`,
			userID, cursorID, limit,
		)
	} else {
		// Previous page: IDs greater than cursor (ascending order, then reverse)
		rows, err = s.db.Query(
			`SELECT `+uploadColumns+` FROM uploads WHERE user_id = ? AND id > ? ORDER BY id ASC LIMIT ?`,
			userID, cursorID, limit,
		)
	}
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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse results for backward pagination
	if !forward && len(uploads) > 0 {
		for i, j := 0, len(uploads)-1; i < j; i, j = i+1, j-1 {
			uploads[i], uploads[j] = uploads[j], uploads[i]
		}
	}

	return uploads, nil
}

// DequeueNext atomically claims the next queued upload for processing.
// Skips uploads in gas backoff (backoff_until in the future).
// Returns nil, nil if no uploads are queued.
func (s *UploadService) DequeueNext() (*Upload, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM uploads WHERE status = 'queued'
		 AND (backoff_until IS NULL OR backoff_until <= CURRENT_TIMESTAMP)
		 ORDER BY queued_at ASC LIMIT 1`,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	_, err = tx.Exec(
		`UPDATE uploads SET status = 'processing', processing_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'queued'`,
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

// MarkCompleted transitions a private upload to "completed" with the DataMap and cost.
// The dataMap is the hex-encoded serialized DataMap returned by antd's finalize endpoint.
func (s *UploadService) MarkCompleted(id int64, dataMap, actualCost string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'completed', data_map = ?, actual_cost = ?, cache_key = ?, completed_at = CURRENT_TIMESTAMP, temp_path = NULL WHERE id = ?`,
		dataMap, actualCost, downloadcache.KeyForIdentifier(dataMap), id,
	)
	return err
}

// MarkCompletedPublic transitions a public upload to "completed" with the on-network
// DataMap address and cost. datamapAddress is the hex-encoded network address returned
// in antd's finalize response when prepare was called with visibility:"public".
// The DataMap chunk itself was already published as part of the same external-signer
// payment batch — no separate daemon-wallet payment.
func (s *UploadService) MarkCompletedPublic(id int64, datamapAddress, actualCost string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'completed', datamap_address = ?, actual_cost = ?, cache_key = ?, completed_at = CURRENT_TIMESTAMP, temp_path = NULL WHERE id = ?`,
		datamapAddress, actualCost, downloadcache.KeyForIdentifier(datamapAddress), id,
	)
	return err
}

// MarkAlreadyStored transitions a private upload to "already_stored": the
// content was already on the network (finalize reported 0 new chunks), so it's
// an idempotent no-op re-upload and nothing was paid. Mirrors MarkCompleted but
// with the dedup status so the UI shows "already on network" rather than a
// fresh store (V2-399).
func (s *UploadService) MarkAlreadyStored(id int64, dataMap, actualCost string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'already_stored', data_map = ?, actual_cost = ?, cache_key = ?, completed_at = CURRENT_TIMESTAMP, temp_path = NULL WHERE id = ?`,
		dataMap, actualCost, downloadcache.KeyForIdentifier(dataMap), id,
	)
	return err
}

// MarkAlreadyStoredPublic is MarkCompletedPublic's content-addressed-dedup
// counterpart (V2-399).
func (s *UploadService) MarkAlreadyStoredPublic(id int64, datamapAddress, actualCost string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'already_stored', datamap_address = ?, actual_cost = ?, cache_key = ?, completed_at = CURRENT_TIMESTAMP, temp_path = NULL WHERE id = ?`,
		datamapAddress, actualCost, downloadcache.KeyForIdentifier(datamapAddress), id,
	)
	return err
}

// MarkPublished flips a previously-private completed upload to visibility='public'
// with its now-published DataMap address. The existing data_map column is preserved
// for idempotency and as a belt-and-suspenders fallback — both forms address the
// same content. Used by cmd/migrate-publish-datamaps to back-publish DataMaps of
// uploads created before public visibility shipped.
func (s *UploadService) MarkPublished(id int64, datamapAddress string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET visibility = 'public', datamap_address = ? WHERE id = ?`,
		datamapAddress, id,
	)
	return err
}

// ListPrivatePublishCandidates returns completed private uploads that have a
// locally stored DataMap but no published datamap_address. limit <= 0 means
// no limit. Ordered by ID for deterministic batching.
func (s *UploadService) ListPrivatePublishCandidates(limit int) ([]*Upload, error) {
	q := `SELECT ` + uploadColumns + ` FROM uploads
	      WHERE status = 'completed' AND visibility = 'private'
	        AND datamap_address IS NULL
	        AND data_map IS NOT NULL AND data_map != ''
	      ORDER BY id`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkFailed transitions an upload to "failed" with an error message.
func (s *UploadService) MarkFailed(id int64, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'failed', error_message = ?, failed_at = CURRENT_TIMESTAMP, temp_path = NULL WHERE id = ?`,
		errMsg, id,
	)
	return err
}

// Recoverable status_detail values: a payment was made (or may have been) for an
// upload that did not complete, so its temp source must be preserved — the
// network chunks may be paid + stored and the DataMap is only regenerable from
// that source. ListActiveTempPaths keeps these temp files out of the GC.
const (
	StatusDetailPaidUnfinalized    = "paid_unfinalized"    // payment confirmed, finalize failed
	StatusDetailPaymentUnconfirmed = "payment_unconfirmed" // tx broadcast, receipt timed out
)

// MarkFailedPreserveTemp marks an upload failed but KEEPS its temp file and
// records a status_detail, for the recoverable cases above. Unlike MarkFailed it
// does NOT null temp_path, so a later retry can re-Prepare (zero-cost dedup) and
// recover the DataMap rather than crypto-shredding paid-for private data.
func (s *UploadService) MarkFailedPreserveTemp(id int64, errMsg, detail string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'failed', error_message = ?, status_detail = ?, failed_at = CURRENT_TIMESTAMP WHERE id = ?`,
		errMsg, detail, id,
	)
	return err
}

// SetGasBackoff puts a queued upload into gas backoff, scheduling it for retry later.
// The upload stays status="queued" with status_detail="gas_backoff".
func (s *UploadService) SetGasBackoff(id int64, backoffUntil time.Time, attempt int, quotedCost string) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status = 'queued', status_detail = 'gas_backoff',
		 backoff_until = ?, backoff_attempt = ?, last_quoted_cost = ?,
		 processing_at = NULL
		 WHERE id = ?`,
		backoffUntil.UTC().Format("2006-01-02 15:04:05"), attempt, quotedCost, id,
	)
	return err
}

// ClearBackoff removes backoff state when an upload proceeds normally.
func (s *UploadService) ClearBackoff(id int64) error {
	_, err := s.db.Exec(
		`UPDATE uploads SET status_detail = NULL, backoff_until = NULL WHERE id = ?`, id,
	)
	return err
}

// RequeueStuck finds uploads that have been "processing" for longer than the timeout
// and resets them to "queued". Returns the number of requeued uploads.
func (s *UploadService) RequeueStuck(timeoutMinutes int) (int64, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(timeoutMinutes) * time.Minute).Format("2006-01-02 15:04:05")
	result, err := s.db.Exec(
		`UPDATE uploads SET status = 'queued', processing_at = NULL
		 WHERE status = 'processing'
		 AND processing_at < ?`,
		cutoff,
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

// Cancel transitions a queued or backoff upload to "failed" with a user-initiated message.
func (s *UploadService) Cancel(id int64) error {
	result, err := s.db.Exec(
		`UPDATE uploads SET status = 'failed', error_message = 'Cancelled by user', status_detail = NULL,
		 backoff_until = NULL, failed_at = CURRENT_TIMESTAMP, temp_path = NULL
		 WHERE id = ? AND status IN ('queued', 'processing')`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errors.New("upload cannot be cancelled in its current state")
	}
	return nil
}

// Retry resets a failed upload back to queued for reprocessing.
func (s *UploadService) Retry(id int64) error {
	result, err := s.db.Exec(
		`UPDATE uploads SET status = 'queued', status_detail = NULL, error_message = NULL,
		 backoff_until = NULL, backoff_attempt = 0, last_quoted_cost = NULL,
		 failed_at = NULL, processing_at = NULL, queued_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND status = 'failed'`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errors.New("only failed uploads can be retried")
	}
	return nil
}

// ForceRetry immediately resets a gas-backoff upload to queued, clearing backoff state.
func (s *UploadService) ForceRetry(id int64) error {
	result, err := s.db.Exec(
		`UPDATE uploads SET status = 'queued', status_detail = NULL,
		 backoff_until = NULL, processing_at = NULL
		 WHERE id = ? AND status = 'queued' AND status_detail = 'gas_backoff'`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errors.New("upload is not in gas backoff")
	}
	return nil
}

// Delete permanently removes an upload record. Only allowed for failed or completed uploads.
func (s *UploadService) Delete(id int64) error {
	// Fan the purge out to the fleet FIRST (V2-873): append the upload's
	// cache keys to cache_purge_log before anything is deleted, so a failure
	// later in this sequence can at worst cause a spurious purge (an instance
	// drops a live entry and re-warms) — never a missed one. Every instance's
	// cache sweep worker consumes this log each tick; the handling instance
	// additionally purges synchronously in the handler (V2-824).
	if u, err := s.GetByID(id); err == nil {
		for _, key := range u.CacheKeys() {
			if _, err := s.db.Exec(`INSERT INTO cache_purge_log (cache_key) VALUES (?)`, key); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, ErrUploadNotFound) {
		return err
	}

	// Clean up related data first
	if _, err := s.db.Exec(`DELETE FROM file_tags WHERE upload_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM collection_files WHERE upload_id = ?`, id); err != nil {
		return err
	}

	result, err := s.db.Exec(
		`DELETE FROM uploads WHERE id = ? AND status IN ('failed', 'completed')`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errors.New("only failed or completed uploads can be deleted")
	}
	return nil
}

// CacheKeys returns every download-cache key this upload's bytes could live
// under, serve-path derivation first: the serve path (downloadETag) prefers
// the local DataMap when one exists, while public upload seeding keys on the
// network address — a purge must cover both.
func (u *Upload) CacheKeys() []string {
	var keys []string
	for _, id := range []sql.NullString{u.DataMap, u.DatamapAddress} {
		if !id.Valid || id.String == "" {
			continue
		}
		k := downloadcache.KeyForIdentifier(id.String)
		if len(keys) == 0 || keys[0] != k {
			keys = append(keys, k)
		}
	}
	return keys
}

// PurgeLogEntry is one consumed row of cache_purge_log.
type PurgeLogEntry struct {
	ID       int64
	CacheKey string
}

// PurgeLogSince returns up to limit purge-log rows with id > after, oldest
// first — the sweep worker's per-tick tail read.
func (s *UploadService) PurgeLogSince(after int64, limit int) ([]PurgeLogEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, cache_key FROM cache_purge_log WHERE id > ? ORDER BY id LIMIT ?`,
		after, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PurgeLogEntry
	for rows.Next() {
		var e PurgeLogEntry
		if err := rows.Scan(&e.ID, &e.CacheKey); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MaxPurgeLogID returns the current high-water mark of cache_purge_log (0 on
// an empty log). Boot uses it to skip history: everything at or before it is
// covered by the boot reconciliation pass.
func (s *UploadService) MaxPurgeLogID() (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(id) FROM cache_purge_log`).Scan(&id); err != nil {
		return 0, err
	}
	return id.Int64, nil
}

// PrunePurgeLog deletes purge-log rows older than before. Writer-role only
// (reader discipline, V2-514); instances offline past the retention window
// are covered by boot reconciliation.
func (s *UploadService) PrunePurgeLog(before time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM cache_purge_log WHERE deleted_at < ?`,
		before.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// LiveCacheKeys reports which of the given cache keys belong to a live upload
// row — the boot-reconciliation lookup (uploads.cache_key is indexed).
func (s *UploadService) LiveCacheKeys(keys []string) (map[string]bool, error) {
	live := make(map[string]bool, len(keys))
	const chunk = 500
	for start := 0; start < len(keys); start += chunk {
		end := start + chunk
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		placeholders := make([]byte, 0, len(batch)*2)
		args := make([]any, 0, len(batch))
		for i, k := range batch {
			if i > 0 {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '?')
			args = append(args, k)
		}
		rows, err := s.db.Query(
			`SELECT cache_key FROM uploads WHERE cache_key IN (`+string(placeholders)+`)`, args...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return nil, err
			}
			live[k] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return live, nil
}

// BackfillCacheKeys computes uploads.cache_key for rows stored before the
// column existed (V2-873 migration 013). Writer-boot singleton — the digest
// is Go-side (KeyForIdentifier), so SQL backfill in the migration was not
// possible. Idempotent; returns how many rows were stamped.
func (s *UploadService) BackfillCacheKeys() (int64, error) {
	rows, err := s.db.Query(
		`SELECT id, data_map, datamap_address FROM uploads
		 WHERE cache_key IS NULL
		 AND ((data_map IS NOT NULL AND data_map != '') OR (datamap_address IS NOT NULL AND datamap_address != ''))`,
	)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id  int64
		key string
	}
	var todo []pending
	for rows.Next() {
		var id int64
		var dataMap, addr sql.NullString
		if err := rows.Scan(&id, &dataMap, &addr); err != nil {
			rows.Close()
			return 0, err
		}
		identifier := dataMap.String
		if identifier == "" {
			identifier = addr.String
		}
		if identifier != "" {
			todo = append(todo, pending{id: id, key: downloadcache.KeyForIdentifier(identifier)})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	var n int64
	for _, p := range todo {
		if _, err := s.db.Exec(`UPDATE uploads SET cache_key = ? WHERE id = ?`, p.key, p.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ListActiveTempPaths returns all temp_path values for uploads still in queued or processing state.
func (s *UploadService) ListActiveTempPaths() ([]string, error) {
	// Keep temp files for in-flight uploads AND for recoverable failures (a
	// payment was/maybe made and the source is still needed for reconciliation),
	// so the temp GC never shreds paid-for data.
	rows, err := s.db.Query(`SELECT temp_path FROM uploads
		WHERE temp_path IS NOT NULL AND temp_path != ''
		AND (status IN ('queued', 'processing')
		     OR status_detail IN ('` + StatusDetailPaidUnfinalized + `', '` + StatusDetailPaymentUnconfirmed + `'))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

func scanUploads(rows *sql.Rows, total int64) ([]*Upload, int64, error) {
	var uploads []*Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, 0, err
		}
		uploads = append(uploads, u)
	}
	return uploads, total, rows.Err()
}
