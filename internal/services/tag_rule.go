package services

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrTagRuleNotFound    = errors.New("tag rule not found")
	ErrInvalidMatchField  = errors.New("match_field must be one of: content_type, filename, file_size, visibility")
	ErrInvalidMatchOp     = errors.New("match_op is not valid for the given match_field")
	ErrInvalidApplyKey    = errors.New("apply_key must match ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$")
)

// validApplyKey matches a tag key: starts with alphanumeric, then up to 62 more
// alphanumeric, dot, underscore, or hyphen characters.
var validApplyKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// validOpsForField defines which match_op values are legal for each match_field.
var validOpsForField = map[string]map[string]bool{
	"content_type": {"equals": true, "regex": true, "contains": true},
	"filename":     {"equals": true, "regex": true, "contains": true},
	"file_size":    {"gt": true, "lt": true},
	"visibility":   {"equals": true},
}

// TagRule represents an auto-tag rule that automatically applies tags to uploads
// when the upload's attributes match the rule's criteria.
type TagRule struct {
	ID          int64
	Name        string
	Description string
	MatchField  string
	MatchOp     string
	MatchValue  string
	ApplyKey    string
	ApplyValue  string
	Priority    int
	IsEnabled   bool
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TagRuleService handles auto-tag rule operations.
type TagRuleService struct {
	db *sql.DB

	// regexCache caches compiled regexes keyed by pattern string.
	regexCache sync.Map
}

// NewTagRuleService creates a new TagRuleService.
func NewTagRuleService(db *sql.DB) *TagRuleService {
	return &TagRuleService{db: db}
}

// validateRule checks that match_field, match_op, and apply_key are valid.
func validateRule(matchField, matchOp, applyKey string) error {
	ops, ok := validOpsForField[matchField]
	if !ok {
		return ErrInvalidMatchField
	}
	if !ops[matchOp] {
		return fmt.Errorf("%w: %q is not valid for field %q", ErrInvalidMatchOp, matchOp, matchField)
	}
	if !validApplyKey.MatchString(applyKey) {
		return ErrInvalidApplyKey
	}
	return nil
}

// Create adds a new tag rule.
func (s *TagRuleService) Create(name, description, matchField, matchOp, matchValue, applyKey, applyValue string, priority int, createdBy int64) (*TagRule, error) {
	if err := validateRule(matchField, matchOp, applyKey); err != nil {
		return nil, err
	}

	result, err := s.db.Exec(
		`INSERT INTO tag_rules (name, description, match_field, match_op, match_value, apply_key, apply_value, priority, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, description, matchField, matchOp, matchValue, applyKey, applyValue, priority, createdBy,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

// GetByID returns a single tag rule by ID.
func (s *TagRuleService) GetByID(id int64) (*TagRule, error) {
	row := s.db.QueryRow(
		`SELECT id, name, description, match_field, match_op, match_value, apply_key, apply_value,
		        priority, is_enabled, created_by, created_at, updated_at
		 FROM tag_rules WHERE id = ?`, id,
	)

	r, err := scanTagRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTagRuleNotFound
		}
		return nil, err
	}
	return r, nil
}

// List returns all tag rules ordered by priority ASC (lowest number first).
func (s *TagRuleService) List() ([]*TagRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, description, match_field, match_op, match_value, apply_key, apply_value,
		        priority, is_enabled, created_by, created_at, updated_at
		 FROM tag_rules ORDER BY priority ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*TagRule
	for rows.Next() {
		r, err := scanTagRuleRows(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// Update modifies an existing tag rule.
func (s *TagRuleService) Update(id int64, name, description, matchField, matchOp, matchValue, applyKey, applyValue string, priority int, isEnabled bool) (*TagRule, error) {
	if err := validateRule(matchField, matchOp, applyKey); err != nil {
		return nil, err
	}

	enabledInt := 0
	if isEnabled {
		enabledInt = 1
	}

	result, err := s.db.Exec(
		`UPDATE tag_rules SET name = ?, description = ?, match_field = ?, match_op = ?, match_value = ?,
		        apply_key = ?, apply_value = ?, priority = ?, is_enabled = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		name, description, matchField, matchOp, matchValue, applyKey, applyValue, priority, enabledInt, id,
	)
	if err != nil {
		return nil, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, ErrTagRuleNotFound
	}

	return s.GetByID(id)
}

// Delete removes a tag rule by ID.
func (s *TagRuleService) Delete(id int64) error {
	result, err := s.db.Exec(`DELETE FROM tag_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrTagRuleNotFound
	}
	return nil
}

// EvaluateRules checks all enabled rules against an upload's attributes and
// returns a map of tag keys to values that should be applied.
// Lower priority numbers take precedence: if two rules produce the same key,
// the one with the lower priority number wins.
func (s *TagRuleService) EvaluateRules(filename, contentType string, fileSize int64, visibility string) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT id, name, description, match_field, match_op, match_value, apply_key, apply_value,
		        priority, is_enabled, created_by, created_at, updated_at
		 FROM tag_rules WHERE is_enabled = 1 ORDER BY priority ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make(map[string]string)

	for rows.Next() {
		r, err := scanTagRuleRows(rows)
		if err != nil {
			return nil, err
		}

		// Skip if this key was already set by a higher-priority (lower number) rule.
		if _, exists := tags[r.ApplyKey]; exists {
			continue
		}

		if s.ruleMatches(r, filename, contentType, fileSize, visibility) {
			tags[r.ApplyKey] = r.ApplyValue
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}

// ruleMatches checks whether a single rule matches the given upload attributes.
func (s *TagRuleService) ruleMatches(r *TagRule, filename, contentType string, fileSize int64, visibility string) bool {
	switch r.MatchField {
	case "content_type":
		return s.matchString(r, contentType)
	case "filename":
		return s.matchString(r, filename)
	case "file_size":
		return s.matchSize(r, fileSize)
	case "visibility":
		return r.MatchOp == "equals" && visibility == r.MatchValue
	default:
		return false
	}
}

// matchString handles equals, contains, and regex matching for string fields.
func (s *TagRuleService) matchString(r *TagRule, value string) bool {
	switch r.MatchOp {
	case "equals":
		return value == r.MatchValue
	case "contains":
		return strings.Contains(value, r.MatchValue)
	case "regex":
		return s.matchRegex(r.MatchValue, value, r.ID)
	default:
		return false
	}
}

// matchSize handles gt and lt comparisons for file_size.
func (s *TagRuleService) matchSize(r *TagRule, fileSize int64) bool {
	threshold, err := strconv.ParseInt(r.MatchValue, 10, 64)
	if err != nil {
		slog.Warn("tag rule has non-numeric match_value for file_size",
			"rule_id", r.ID, "match_value", r.MatchValue, "error", err)
		return false
	}

	switch r.MatchOp {
	case "gt":
		return fileSize > threshold
	case "lt":
		return fileSize < threshold
	default:
		return false
	}
}

// matchRegex compiles (and caches) the pattern and tests it against value.
// Invalid regex patterns are logged and treated as non-matching.
func (s *TagRuleService) matchRegex(pattern, value string, ruleID int64) bool {
	re, err := s.getOrCompileRegex(pattern)
	if err != nil {
		slog.Warn("tag rule has invalid regex pattern",
			"rule_id", ruleID, "pattern", pattern, "error", err)
		return false
	}
	return re.MatchString(value)
}

// getOrCompileRegex returns a cached compiled regex, or compiles and caches it.
func (s *TagRuleService) getOrCompileRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := s.regexCache.Load(pattern); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re, nil
		}
		// Cached error sentinel — the pattern was invalid previously.
		return nil, fmt.Errorf("previously failed to compile regex: %s", pattern)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		// Store a nil sentinel so we don't retry compilation on every evaluation.
		s.regexCache.Store(pattern, err.Error())
		return nil, err
	}

	s.regexCache.Store(pattern, re)
	return re, nil
}

// scanTagRule scans a single row from QueryRow into a TagRule.
func scanTagRule(row *sql.Row) (*TagRule, error) {
	r := &TagRule{}
	var isEnabled int
	err := row.Scan(
		&r.ID, &r.Name, &r.Description, &r.MatchField, &r.MatchOp, &r.MatchValue,
		&r.ApplyKey, &r.ApplyValue, &r.Priority, &isEnabled, &r.CreatedBy,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	r.IsEnabled = isEnabled != 0
	return r, nil
}

// scanTagRuleRows scans a single row from Rows into a TagRule.
func scanTagRuleRows(rows *sql.Rows) (*TagRule, error) {
	r := &TagRule{}
	var isEnabled int
	err := rows.Scan(
		&r.ID, &r.Name, &r.Description, &r.MatchField, &r.MatchOp, &r.MatchValue,
		&r.ApplyKey, &r.ApplyValue, &r.Priority, &isEnabled, &r.CreatedBy,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	r.IsEnabled = isEnabled != 0
	return r, nil
}
