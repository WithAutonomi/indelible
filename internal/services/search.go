package services

import (
	"strconv"
	"strings"

	"github.com/WithAutonomi/indelible/internal/database"
)

// SearchService backs the global search omnibox (V2-406). It runs simple SQL
// LIKE queries across entities and returns a generic, typed hit shape so the
// matching backend could be swapped/upgraded later without changing the API
// contract. No external search engine is involved.
type SearchService struct {
	db *database.DB
}

func NewSearchService(db *database.DB) *SearchService {
	return &SearchService{db: db}
}

// SearchHit is one generic result. Type is the entity kind ("file",
// "collection", "tag", "user", "token", "webhook"); ID is the identifier the
// frontend needs to navigate to it (uuid or numeric id, or "key=value" for a
// tag); Label/Sublabel are display text.
type SearchHit struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
}

func likePattern(q string) string {
	// Escape LIKE wildcards in user input so they're matched literally.
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return "%" + r.Replace(q) + "%"
}

// Files searches the caller's own uploads by filename.
func (s *SearchService) Files(userID int64, q string, limit int) ([]SearchHit, error) {
	like := likePattern(q)
	rows, err := s.db.Query(
		`SELECT uuid, original_filename, status FROM uploads
		 WHERE user_id = ? AND (original_filename LIKE ? ESCAPE '\' OR filename LIKE ? ESCAPE '\')
		 ORDER BY created_at DESC LIMIT ?`,
		userID, like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var uuid, name, status string
		if err := rows.Scan(&uuid, &name, &status); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Type: "file", ID: uuid, Label: name, Sublabel: status})
	}
	return hits, rows.Err()
}

// Collections searches the caller's own collections by name/description.
func (s *SearchService) Collections(userID int64, q string, limit int) ([]SearchHit, error) {
	like := likePattern(q)
	rows, err := s.db.Query(
		`SELECT id, name, description FROM collections
		 WHERE created_by = ? AND (name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')
		 ORDER BY name ASC LIMIT ?`,
		userID, like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id int64
		var name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Type: "collection", ID: strconv.FormatInt(id, 10), Label: name, Sublabel: desc})
	}
	return hits, rows.Err()
}

// Tags searches distinct key/value pairs on the caller's own uploads.
func (s *SearchService) Tags(userID int64, q string, limit int) ([]SearchHit, error) {
	like := likePattern(q)
	rows, err := s.db.Query(
		`SELECT DISTINCT ft.tag_key, ft.tag_value FROM file_tags ft
		 INNER JOIN uploads u ON ft.upload_id = u.id
		 WHERE u.user_id = ? AND (ft.tag_key LIKE ? ESCAPE '\' OR ft.tag_value LIKE ? ESCAPE '\')
		 ORDER BY ft.tag_key, ft.tag_value LIMIT ?`,
		userID, like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Type: "tag", ID: key + "=" + value, Label: key + ": " + value})
	}
	return hits, rows.Err()
}

// Users searches active users by email/name. Admin scope only — the caller must
// be gated by the handler before this is invoked.
func (s *SearchService) Users(q string, limit int) ([]SearchHit, error) {
	like := likePattern(q)
	rows, err := s.db.Query(
		`SELECT id, email, first_name, last_name FROM users
		 WHERE deleted_at IS NULL
		   AND (email LIKE ? ESCAPE '\' OR first_name LIKE ? ESCAPE '\' OR last_name LIKE ? ESCAPE '\')
		 ORDER BY email ASC LIMIT ?`,
		like, like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id int64
		var email, first, last string
		if err := rows.Scan(&id, &email, &first, &last); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Type: "user", ID: strconv.FormatInt(id, 10), Label: email, Sublabel: strings.TrimSpace(first + " " + last)})
	}
	return hits, rows.Err()
}

// Tokens searches API tokens by name/description. Admin scope only. Never selects
// token_hash, so no secret material is exposed.
func (s *SearchService) Tokens(q string, limit int) ([]SearchHit, error) {
	like := likePattern(q)
	rows, err := s.db.Query(
		`SELECT uuid, name, description FROM api_tokens
		 WHERE name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\'
		 ORDER BY created_at DESC LIMIT ?`,
		like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var uuid, name, desc string
		if err := rows.Scan(&uuid, &name, &desc); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Type: "token", ID: uuid, Label: name, Sublabel: desc})
	}
	return hits, rows.Err()
}

// Webhooks searches webhook configs by URL/integration type. Admin scope only.
func (s *SearchService) Webhooks(q string, limit int) ([]SearchHit, error) {
	like := likePattern(q)
	rows, err := s.db.Query(
		`SELECT id, url, integration_type FROM webhook_config
		 WHERE url LIKE ? ESCAPE '\' OR integration_type LIKE ? ESCAPE '\'
		 ORDER BY created_at ASC LIMIT ?`,
		like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id int64
		var url, itype string
		if err := rows.Scan(&id, &url, &itype); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Type: "webhook", ID: strconv.FormatInt(id, 10), Label: url, Sublabel: itype})
	}
	return hits, rows.Err()
}
