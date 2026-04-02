package services

import (
	"fmt"
	"regexp"
	"strings"
)

// Requirement represents a single parsed label selector requirement.
type Requirement struct {
	Key      string
	Operator string   // "=", "!=", "in", "notin", "exists", "notexists"
	Values   []string
}

var (
	keyRegex   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)
	valueRegex = regexp.MustCompile(`^[a-zA-Z0-9/][a-zA-Z0-9._/ -]{0,253}$`)
)

const (
	maxRequirements = 10
	maxValues       = 20
)

// ParseSelector parses a comma-separated label selector string into a slice of
// Requirements. It supports six operator forms:
//
//	key=value        equality
//	key!=value       inequality
//	key in (v1,v2)   set membership
//	key notin (v1,v2) set exclusion
//	key              exists
//	!key             not exists
func ParseSelector(input string) ([]Requirement, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	parts, err := splitSelector(input)
	if err != nil {
		return nil, err
	}

	if len(parts) > maxRequirements {
		return nil, fmt.Errorf("selector has %d requirements, maximum is %d", len(parts), maxRequirements)
	}

	reqs := make([]Requirement, 0, len(parts))
	for _, part := range parts {
		req, err := parseRequirement(part)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, req)
	}
	return reqs, nil
}

// splitSelector splits the top-level selector by commas, respecting
// parenthesised value lists so that commas inside (...) are not treated as
// separators.
func splitSelector(input string) ([]string, error) {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unexpected ')' at position %d", i)
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(input[start:i])
				if part != "" {
					parts = append(parts, part)
				}
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unclosed '(' in selector")
	}
	last := strings.TrimSpace(input[start:])
	if last != "" {
		parts = append(parts, last)
	}
	return parts, nil
}

func parseRequirement(s string) (Requirement, error) {
	s = strings.TrimSpace(s)

	// "!key" → notexists
	if strings.HasPrefix(s, "!") {
		key := strings.TrimSpace(s[1:])
		if err := validateKey(key); err != nil {
			return Requirement{}, err
		}
		return Requirement{Key: key, Operator: "notexists"}, nil
	}

	// "key notin (v1,v2)"
	if idx := indexOfOperatorWord(s, " notin "); idx >= 0 {
		return parseSetRequirement(s, idx, " notin ", "notin")
	}

	// "key in (v1,v2)"
	if idx := indexOfOperatorWord(s, " in "); idx >= 0 {
		return parseSetRequirement(s, idx, " in ", "in")
	}

	// "key!=value"
	if idx := strings.Index(s, "!="); idx >= 0 {
		return parseComparisonRequirement(s, idx, "!=", 2)
	}

	// "key=value"
	if idx := strings.Index(s, "="); idx >= 0 {
		return parseComparisonRequirement(s, idx, "=", 1)
	}

	// bare "key" → exists
	if err := validateKey(s); err != nil {
		return Requirement{}, err
	}
	return Requirement{Key: s, Operator: "exists"}, nil
}

// indexOfOperatorWord returns the index of the operator keyword in s, or -1.
// It requires the keyword to appear surrounded by spaces (as embedded in the
// pattern) so that keys like "myinkey" are not misinterpreted.
func indexOfOperatorWord(s, keyword string) int {
	return strings.Index(s, keyword)
}

func parseComparisonRequirement(s string, idx int, op string, opLen int) (Requirement, error) {
	key := strings.TrimSpace(s[:idx])
	value := strings.TrimSpace(s[idx+opLen:])
	if err := validateKey(key); err != nil {
		return Requirement{}, err
	}
	if err := validateValue(value); err != nil {
		return Requirement{}, err
	}
	return Requirement{Key: key, Operator: op, Values: []string{value}}, nil
}

func parseSetRequirement(s string, idx int, keyword, op string) (Requirement, error) {
	key := strings.TrimSpace(s[:idx])
	rest := strings.TrimSpace(s[idx+len(keyword):])

	if err := validateKey(key); err != nil {
		return Requirement{}, err
	}

	if !strings.HasPrefix(rest, "(") || !strings.HasSuffix(rest, ")") {
		return Requirement{}, fmt.Errorf("%s operator requires parenthesised value list, got %q", op, rest)
	}
	inner := rest[1 : len(rest)-1]
	rawVals := strings.Split(inner, ",")
	if len(rawVals) == 0 {
		return Requirement{}, fmt.Errorf("%s operator requires at least one value", op)
	}
	if len(rawVals) > maxValues {
		return Requirement{}, fmt.Errorf("%s operator has %d values, maximum is %d", op, len(rawVals), maxValues)
	}

	values := make([]string, 0, len(rawVals))
	for _, v := range rawVals {
		v = strings.TrimSpace(v)
		if v == "" {
			return Requirement{}, fmt.Errorf("%s operator contains empty value", op)
		}
		if err := validateValue(v); err != nil {
			return Requirement{}, err
		}
		values = append(values, v)
	}

	return Requirement{Key: key, Operator: op, Values: values}, nil
}

func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key must not be empty")
	}
	if !keyRegex.MatchString(key) {
		return fmt.Errorf("invalid key %q: must match %s", key, keyRegex.String())
	}
	return nil
}

func validateValue(value string) error {
	if value == "" {
		return fmt.Errorf("value must not be empty")
	}
	if !valueRegex.MatchString(value) {
		return fmt.Errorf("invalid value %q: must match %s", value, valueRegex.String())
	}
	return nil
}

// BuildSelectorSQL converts parsed requirements into SQL WHERE clause fragments
// using EXISTS / NOT EXISTS subqueries against the file_tags table. The uploads
// table is expected to be aliased as "u".
func BuildSelectorSQL(reqs []Requirement) (clauses []string, args []interface{}) {
	for _, r := range reqs {
		var clause string
		switch r.Operator {
		case "=":
			clause = "EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ? AND ft.tag_value = ?)"
			args = append(args, r.Key, r.Values[0])

		case "!=":
			clause = "NOT EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ? AND ft.tag_value = ?)"
			args = append(args, r.Key, r.Values[0])

		case "in":
			placeholders := make([]string, len(r.Values))
			for i := range r.Values {
				placeholders[i] = "?"
			}
			clause = fmt.Sprintf(
				"EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ? AND ft.tag_value IN (%s))",
				strings.Join(placeholders, ","),
			)
			args = append(args, r.Key)
			for _, v := range r.Values {
				args = append(args, v)
			}

		case "notin":
			placeholders := make([]string, len(r.Values))
			for i := range r.Values {
				placeholders[i] = "?"
			}
			clause = fmt.Sprintf(
				"NOT EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ? AND ft.tag_value IN (%s))",
				strings.Join(placeholders, ","),
			)
			args = append(args, r.Key)
			for _, v := range r.Values {
				args = append(args, v)
			}

		case "exists":
			clause = "EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ?)"
			args = append(args, r.Key)

		case "notexists":
			clause = "NOT EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ?)"
			args = append(args, r.Key)
		}
		clauses = append(clauses, clause)
	}
	return clauses, args
}
