package services

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

// The uploads export is an NDJSON stream: a single header line followed by one
// JSON object per upload. Streaming keeps server memory bounded regardless of
// catalog size — we never buffer the whole catalog or any file contents, only a
// page of metadata at a time.
const (
	uploadsExportKind   = "indelible-uploads-export"
	uploadsExportSchema = 1
	exportBatchSize     = 500
)

// BackupService produces and consumes the upload-catalog + DataMap export used
// for disaster recovery and instance migration. The export intentionally carries
// every private DataMap (the locally-held retrieval secret) — see
// docs/guides/backup-restore.md, "treat the export as secret-grade".
type BackupService struct {
	db *database.DB
}

// NewBackupService creates a new BackupService.
func NewBackupService(db *database.DB) *BackupService {
	return &BackupService{db: db}
}

// ExportHeader is the first line of an uploads export.
type ExportHeader struct {
	Kind       string    `json:"kind"`
	Schema     int       `json:"schema"`
	ExportedAt time.Time `json:"exported_at"`
	Count      int64     `json:"count"`
}

// ExportUpload is one upload record line. It carries everything needed to
// restore retrieval of the file on a fresh instance — for a private upload that
// means DataMap, the locally-held retrieval secret.
type ExportUpload struct {
	UUID             string              `json:"uuid"`
	OwnerEmail       string              `json:"owner_email,omitempty"`
	Filename         string              `json:"filename"`
	OriginalFilename string              `json:"original_filename"`
	ContentType      string              `json:"content_type,omitempty"`
	FileSize         int64               `json:"file_size"`
	Visibility       string              `json:"visibility"`
	Status           string              `json:"status"`
	DataMap          string              `json:"data_map,omitempty"`
	DatamapAddress   string              `json:"datamap_address,omitempty"`
	ActualCost       string              `json:"actual_cost,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	CompletedAt      *time.Time          `json:"completed_at,omitempty"`
	Tags             map[string][]string `json:"tags,omitempty"`
	Collections      []string            `json:"collections,omitempty"`
}

// ImportResult summarises an import run.
type ImportResult struct {
	Imported      int      `json:"imported"`
	Skipped       int      `json:"skipped"`        // uuid already present on target
	OwnerFallback int      `json:"owner_fallback"` // owner email unmatched → assigned to importer
	Errors        []string `json:"errors,omitempty"`
}

// ExportUploads streams the full upload catalog to w as NDJSON (header line +
// one object per upload) and returns the upload count written. Errors before the
// first byte is written are returned; once streaming starts, a mid-stream error
// is returned too but the response is already partially written (a valid NDJSON
// prefix).
func (s *BackupService) ExportUploads(w io.Writer) (int64, error) {
	var count int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM uploads`).Scan(&count); err != nil {
		return 0, err
	}
	emails, err := s.emailByID()
	if err != nil {
		return 0, err
	}

	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	if err := enc.Encode(ExportHeader{
		Kind:       uploadsExportKind,
		Schema:     uploadsExportSchema,
		ExportedAt: time.Now().UTC(),
		Count:      count,
	}); err != nil {
		return 0, err
	}

	var lastID int64
	var written int64
	for {
		rows, err := s.db.Query(
			`SELECT `+uploadColumns+` FROM uploads WHERE id > ? ORDER BY id ASC LIMIT ?`,
			lastID, exportBatchSize,
		)
		if err != nil {
			return written, err
		}
		var batch []*Upload
		for rows.Next() {
			u, err := scanUpload(rows)
			if err != nil {
				rows.Close()
				return written, err
			}
			batch = append(batch, u)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return written, err
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}

		ids := make([]int64, len(batch))
		for i, u := range batch {
			ids[i] = u.ID
		}
		tags, err := s.tagsForUploads(ids)
		if err != nil {
			return written, err
		}
		colls, err := s.collectionsForUploads(ids)
		if err != nil {
			return written, err
		}

		for _, u := range batch {
			rec := ExportUpload{
				UUID:             u.UUID,
				OwnerEmail:       emails[u.UserID],
				Filename:         u.Filename,
				OriginalFilename: u.OriginalFilename,
				ContentType:      u.ContentType,
				FileSize:         u.FileSize,
				Visibility:       u.Visibility,
				Status:           u.Status,
				DataMap:          u.DataMap.String,
				DatamapAddress:   u.DatamapAddress.String,
				ActualCost:       u.ActualCost.String,
				CreatedAt:        u.CreatedAt,
				Tags:             tags[u.ID],
				Collections:      colls[u.ID],
			}
			if u.CompletedAt.Valid {
				t := u.CompletedAt.Time
				rec.CompletedAt = &t
			}
			if err := enc.Encode(rec); err != nil {
				return written, err
			}
			written++
		}
		lastID = batch[len(batch)-1].ID
	}

	return written, bw.Flush()
}

// ImportUploads reads an NDJSON export from r and recreates upload records on
// this instance, returning a summary. Records are inserted with their original
// status, so completed uploads are immediately retrievable and the upload worker
// (which only picks up "queued"/"processing") never re-uploads or re-pays. A
// uuid already present on the target is skipped (idempotent restore). Owners are
// resolved by email, falling back to importerID when unmatched.
func (s *BackupService) ImportUploads(r io.Reader, importerID int64) (*ImportResult, error) {
	emailToID, err := s.idByEmail()
	if err != nil {
		return nil, err
	}

	res := &ImportResult{}
	sc := bufio.NewScanner(r)
	// DataMaps make individual lines large; allow up to 8 MiB per line.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	headerSeen := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !headerSeen {
			var h ExportHeader
			if err := json.Unmarshal([]byte(line), &h); err != nil || h.Kind != uploadsExportKind {
				return nil, fmt.Errorf("not a valid %s: first line is not a recognised header", uploadsExportKind)
			}
			if h.Schema != uploadsExportSchema {
				return nil, fmt.Errorf("unsupported export schema %d (this build supports %d)", h.Schema, uploadsExportSchema)
			}
			headerSeen = true
			continue
		}

		var eu ExportUpload
		if err := json.Unmarshal([]byte(line), &eu); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("uuid ?: bad JSON line: %v", err))
			continue
		}
		if err := s.importOne(&eu, importerID, emailToID, res); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("uuid %s: %v", eu.UUID, err))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading export: %w", err)
	}
	if !headerSeen {
		return nil, errors.New("empty or invalid export (no header line found)")
	}
	return res, nil
}

func (s *BackupService) importOne(eu *ExportUpload, importerID int64, emailToID map[string]int64, res *ImportResult) error {
	if eu.UUID == "" {
		return errors.New("record has no uuid")
	}

	ownerID := importerID
	if id, ok := emailToID[strings.ToLower(eu.OwnerEmail)]; ok && eu.OwnerEmail != "" {
		ownerID = id
	} else {
		res.OwnerFallback++
	}

	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM uploads WHERE uuid = ?)`, eu.UUID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		res.Skipped++
		return nil
	}

	createdAt := eu.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	var newID int64
	err := s.db.QueryRow(
		`INSERT INTO uploads
		   (uuid, user_id, filename, original_filename, file_size, content_type, visibility, status,
		    datamap_address, data_map, actual_cost, queued_at, completed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		eu.UUID, ownerID, eu.Filename, eu.OriginalFilename, eu.FileSize, eu.ContentType, eu.Visibility, eu.Status,
		nullStr(eu.DatamapAddress), nullStr(eu.DataMap), nullStr(eu.ActualCost),
		createdAt, toNullTime(eu.CompletedAt), createdAt,
	).Scan(&newID)
	if err != nil {
		return err
	}
	res.Imported++

	// Tags and collection membership are restorative extras: a failure here does
	// not undo the upload row (retrieval already works), it is just reported.
	for k, vals := range eu.Tags {
		for _, v := range vals {
			if _, err := s.db.Exec(`INSERT INTO file_tags (upload_id, tag_key, tag_value) VALUES (?, ?, ?)`, newID, k, v); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("uuid %s: tag %q: %v", eu.UUID, k, err))
			}
		}
	}
	for _, name := range eu.Collections {
		cid, err := s.findOrCreateCollection(name, ownerID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("uuid %s: collection %q: %v", eu.UUID, name, err))
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO collection_files (collection_id, upload_id) VALUES (?, ?)`, cid, newID); err != nil && !isUniqueViolation(err) {
			res.Errors = append(res.Errors, fmt.Sprintf("uuid %s: collection %q membership: %v", eu.UUID, name, err))
		}
	}
	return nil
}

// findOrCreateCollection returns the id of a top-level collection owned by
// ownerID with the given name, creating it if absent. Collection hierarchy is
// flattened on import (documented limitation).
func (s *BackupService) findOrCreateCollection(name string, ownerID int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM collections WHERE name = ? AND created_by = ? AND parent_id IS NULL`, name, ownerID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = s.db.QueryRow(
		`INSERT INTO collections (name, description, parent_id, created_by) VALUES (?, '', NULL, ?) RETURNING id`,
		name, ownerID,
	).Scan(&id)
	return id, err
}

// emailByID returns a userID→email map for resolving owners on export.
func (s *BackupService) emailByID() (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT id, email FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]string)
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, rows.Err()
}

// idByEmail returns a lowercased-email→userID map for resolving owners on import.
func (s *BackupService) idByEmail() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT id, email FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[strings.ToLower(email)] = id
	}
	return out, rows.Err()
}

func (s *BackupService) tagsForUploads(ids []int64) (map[int64]map[string][]string, error) {
	out := make(map[int64]map[string][]string)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT upload_id, tag_key, tag_value FROM file_tags WHERE upload_id IN (`+placeholders(len(ids))+`) ORDER BY upload_id, tag_key, id`,
		int64sToArgs(ids)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var k, v string
		if err := rows.Scan(&uid, &k, &v); err != nil {
			return nil, err
		}
		m := out[uid]
		if m == nil {
			m = make(map[string][]string)
			out[uid] = m
		}
		m[k] = append(m[k], v)
	}
	return out, rows.Err()
}

func (s *BackupService) collectionsForUploads(ids []int64) (map[int64][]string, error) {
	out := make(map[int64][]string)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT cf.upload_id, c.name FROM collection_files cf
		 INNER JOIN collections c ON c.id = cf.collection_id
		 WHERE cf.upload_id IN (`+placeholders(len(ids))+`) ORDER BY cf.upload_id, c.name`,
		int64sToArgs(ids)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var name string
		if err := rows.Scan(&uid, &name); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], name)
	}
	return out, rows.Err()
}

func int64sToArgs(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

func nullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func toNullTime(t *time.Time) sql.NullTime {
	if t == nil || t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
