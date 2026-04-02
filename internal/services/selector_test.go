package services

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseSelector tests
// ---------------------------------------------------------------------------

func TestParseSelector_Equality(t *testing.T) {
	reqs, err := ParseSelector("env=production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Key != "env" || r.Operator != "=" || len(r.Values) != 1 || r.Values[0] != "production" {
		t.Fatalf("unexpected requirement: %+v", r)
	}
}

func TestParseSelector_Inequality(t *testing.T) {
	reqs, err := ParseSelector("tier!=free")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Key != "tier" || r.Operator != "!=" || len(r.Values) != 1 || r.Values[0] != "free" {
		t.Fatalf("unexpected requirement: %+v", r)
	}
}

func TestParseSelector_In(t *testing.T) {
	reqs, err := ParseSelector("region in (us-east,eu-west)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Key != "region" || r.Operator != "in" {
		t.Fatalf("unexpected key/op: %+v", r)
	}
	if len(r.Values) != 2 || r.Values[0] != "us-east" || r.Values[1] != "eu-west" {
		t.Fatalf("unexpected values: %v", r.Values)
	}
}

func TestParseSelector_NotIn(t *testing.T) {
	reqs, err := ParseSelector("status notin (archived,deleted)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Key != "status" || r.Operator != "notin" {
		t.Fatalf("unexpected key/op: %+v", r)
	}
	if len(r.Values) != 2 || r.Values[0] != "archived" || r.Values[1] != "deleted" {
		t.Fatalf("unexpected values: %v", r.Values)
	}
}

func TestParseSelector_Exists(t *testing.T) {
	reqs, err := ParseSelector("reviewed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Key != "reviewed" || r.Operator != "exists" || len(r.Values) != 0 {
		t.Fatalf("unexpected requirement: %+v", r)
	}
}

func TestParseSelector_NotExists(t *testing.T) {
	reqs, err := ParseSelector("!deprecated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Key != "deprecated" || r.Operator != "notexists" || len(r.Values) != 0 {
		t.Fatalf("unexpected requirement: %+v", r)
	}
}

func TestParseSelector_MultipleRequirements(t *testing.T) {
	reqs, err := ParseSelector("env=production,tier!=free,region in (us-east,eu-west),reviewed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 4 {
		t.Fatalf("expected 4 requirements, got %d", len(reqs))
	}
	if reqs[0].Operator != "=" {
		t.Errorf("req[0]: expected =, got %s", reqs[0].Operator)
	}
	if reqs[1].Operator != "!=" {
		t.Errorf("req[1]: expected !=, got %s", reqs[1].Operator)
	}
	if reqs[2].Operator != "in" {
		t.Errorf("req[2]: expected in, got %s", reqs[2].Operator)
	}
	if reqs[3].Operator != "exists" {
		t.Errorf("req[3]: expected exists, got %s", reqs[3].Operator)
	}
}

func TestParseSelector_EmptyInput(t *testing.T) {
	reqs, err := ParseSelector("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != nil {
		t.Fatalf("expected nil, got %v", reqs)
	}
}

func TestParseSelector_WhitespaceOnly(t *testing.T) {
	reqs, err := ParseSelector("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != nil {
		t.Fatalf("expected nil, got %v", reqs)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestParseSelector_DotsInKey(t *testing.T) {
	reqs, err := ParseSelector("app.kubernetes.io=myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs[0].Key != "app.kubernetes.io" {
		t.Fatalf("unexpected key: %s", reqs[0].Key)
	}
}

func TestParseSelector_DashInKey(t *testing.T) {
	reqs, err := ParseSelector("my-label")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs[0].Key != "my-label" || reqs[0].Operator != "exists" {
		t.Fatalf("unexpected requirement: %+v", reqs[0])
	}
}

func TestParseSelector_SpaceInValue(t *testing.T) {
	reqs, err := ParseSelector("path in (my folder,other dir)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(reqs))
	}
	if reqs[0].Values[0] != "my folder" || reqs[0].Values[1] != "other dir" {
		t.Fatalf("unexpected values: %v", reqs[0].Values)
	}
}

func TestParseSelector_SlashInValue(t *testing.T) {
	reqs, err := ParseSelector("path=/data/uploads/img")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs[0].Values[0] != "/data/uploads/img" {
		t.Fatalf("unexpected value: %s", reqs[0].Values[0])
	}
}

func TestParseSelector_TrimSpaces(t *testing.T) {
	reqs, err := ParseSelector("  env = production , tier != free  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requirements, got %d", len(reqs))
	}
	if reqs[0].Key != "env" || reqs[0].Values[0] != "production" {
		t.Fatalf("req[0] not trimmed: %+v", reqs[0])
	}
	if reqs[1].Key != "tier" || reqs[1].Values[0] != "free" {
		t.Fatalf("req[1] not trimmed: %+v", reqs[1])
	}
}

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------

func TestParseSelector_InvalidKey_Empty(t *testing.T) {
	_, err := ParseSelector("=value")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Fatalf("error should mention key: %v", err)
	}
}

func TestParseSelector_InvalidKey_StartWithDot(t *testing.T) {
	_, err := ParseSelector(".bad=value")
	if err == nil {
		t.Fatal("expected error for key starting with dot")
	}
}

func TestParseSelector_InvalidKey_TooLong(t *testing.T) {
	longKey := "a" + strings.Repeat("b", 63) // 64 chars total, exceeds 63-char max
	_, err := ParseSelector(longKey + "=val")
	if err == nil {
		t.Fatal("expected error for key exceeding 63 characters")
	}
}

func TestParseSelector_InvalidValue_StartWithDot(t *testing.T) {
	_, err := ParseSelector("key=.bad")
	if err == nil {
		t.Fatal("expected error for value starting with dot")
	}
}

func TestParseSelector_TooManyRequirements(t *testing.T) {
	parts := make([]string, 11)
	for i := range parts {
		parts[i] = "key" + string(rune('a'+i))
	}
	_, err := ParseSelector(strings.Join(parts, ","))
	if err == nil {
		t.Fatal("expected error for too many requirements")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("error should mention maximum: %v", err)
	}
}

func TestParseSelector_TooManyValues(t *testing.T) {
	vals := make([]string, 21)
	for i := range vals {
		vals[i] = "v" + strings.Repeat("x", i)
		if vals[i] == "" {
			vals[i] = "vx"
		}
	}
	selector := "key in (" + strings.Join(vals, ",") + ")"
	_, err := ParseSelector(selector)
	if err == nil {
		t.Fatal("expected error for too many values")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("error should mention maximum: %v", err)
	}
}

func TestParseSelector_UnclosedParen(t *testing.T) {
	_, err := ParseSelector("key in (a,b")
	if err == nil {
		t.Fatal("expected error for unclosed parenthesis")
	}
}

func TestParseSelector_EmptyValueInSet(t *testing.T) {
	_, err := ParseSelector("key in (a,,b)")
	if err == nil {
		t.Fatal("expected error for empty value in set")
	}
}

func TestParseSelector_MissingParens(t *testing.T) {
	_, err := ParseSelector("key in a,b")
	if err == nil {
		t.Fatal("expected error for missing parentheses")
	}
}

func TestParseSelector_NotExistsInvalidKey(t *testing.T) {
	_, err := ParseSelector("!.bad")
	if err == nil {
		t.Fatal("expected error for invalid key after !")
	}
}

// ---------------------------------------------------------------------------
// BuildSelectorSQL tests
// ---------------------------------------------------------------------------

func TestBuildSelectorSQL_Equality(t *testing.T) {
	reqs := []Requirement{{Key: "env", Operator: "=", Values: []string{"prod"}}}
	clauses, args := BuildSelectorSQL(reqs)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	expected := "EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ? AND ft.tag_value = ?)"
	if clauses[0] != expected {
		t.Fatalf("clause mismatch:\n  got:  %s\n  want: %s", clauses[0], expected)
	}
	if len(args) != 2 || args[0] != "env" || args[1] != "prod" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildSelectorSQL_Inequality(t *testing.T) {
	reqs := []Requirement{{Key: "tier", Operator: "!=", Values: []string{"free"}}}
	clauses, args := BuildSelectorSQL(reqs)
	if !strings.HasPrefix(clauses[0], "NOT EXISTS") {
		t.Fatalf("expected NOT EXISTS clause, got: %s", clauses[0])
	}
	if len(args) != 2 || args[0] != "tier" || args[1] != "free" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildSelectorSQL_In(t *testing.T) {
	reqs := []Requirement{{Key: "region", Operator: "in", Values: []string{"us", "eu", "ap"}}}
	clauses, args := BuildSelectorSQL(reqs)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if !strings.Contains(clauses[0], "IN (?,?,?)") {
		t.Fatalf("expected 3 placeholders in IN clause, got: %s", clauses[0])
	}
	if len(args) != 4 { // key + 3 values
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "region" || args[1] != "us" || args[2] != "eu" || args[3] != "ap" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildSelectorSQL_NotIn(t *testing.T) {
	reqs := []Requirement{{Key: "status", Operator: "notin", Values: []string{"archived", "deleted"}}}
	clauses, args := BuildSelectorSQL(reqs)
	if !strings.HasPrefix(clauses[0], "NOT EXISTS") {
		t.Fatalf("expected NOT EXISTS clause, got: %s", clauses[0])
	}
	if !strings.Contains(clauses[0], "IN (?,?)") {
		t.Fatalf("expected 2 placeholders, got: %s", clauses[0])
	}
	if len(args) != 3 { // key + 2 values
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
}

func TestBuildSelectorSQL_Exists(t *testing.T) {
	reqs := []Requirement{{Key: "reviewed", Operator: "exists"}}
	clauses, args := BuildSelectorSQL(reqs)
	expected := "EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ?)"
	if clauses[0] != expected {
		t.Fatalf("clause mismatch:\n  got:  %s\n  want: %s", clauses[0], expected)
	}
	if len(args) != 1 || args[0] != "reviewed" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildSelectorSQL_NotExists(t *testing.T) {
	reqs := []Requirement{{Key: "deprecated", Operator: "notexists"}}
	clauses, args := BuildSelectorSQL(reqs)
	expected := "NOT EXISTS (SELECT 1 FROM file_tags ft WHERE ft.upload_id = u.id AND ft.tag_key = ?)"
	if clauses[0] != expected {
		t.Fatalf("clause mismatch:\n  got:  %s\n  want: %s", clauses[0], expected)
	}
	if len(args) != 1 || args[0] != "deprecated" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildSelectorSQL_MultipleClauses(t *testing.T) {
	reqs := []Requirement{
		{Key: "env", Operator: "=", Values: []string{"prod"}},
		{Key: "reviewed", Operator: "exists"},
		{Key: "region", Operator: "in", Values: []string{"us", "eu"}},
	}
	clauses, args := BuildSelectorSQL(reqs)
	if len(clauses) != 3 {
		t.Fatalf("expected 3 clauses, got %d", len(clauses))
	}
	// env=prod: key + value = 2 args
	// reviewed exists: key = 1 arg
	// region in (us,eu): key + 2 values = 3 args
	// total: 6 args
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}
}

func TestBuildSelectorSQL_Empty(t *testing.T) {
	clauses, args := BuildSelectorSQL(nil)
	if len(clauses) != 0 || len(args) != 0 {
		t.Fatalf("expected empty results, got clauses=%v args=%v", clauses, args)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: parse then build SQL
// ---------------------------------------------------------------------------

func TestRoundTrip_ParseThenSQL(t *testing.T) {
	input := "env=production,!deprecated,region in (us-east,eu-west)"
	reqs, err := ParseSelector(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	clauses, args := BuildSelectorSQL(reqs)
	if len(clauses) != 3 {
		t.Fatalf("expected 3 clauses, got %d", len(clauses))
	}
	// env=production: 2 args
	// !deprecated: 1 arg
	// region in (us-east,eu-west): 3 args
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}
}
