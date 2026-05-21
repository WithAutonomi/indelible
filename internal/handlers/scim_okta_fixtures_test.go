package handlers_test

// Tier 1.5 of V2-273: replay real Okta SCIM payloads captured during the
// Tier 2 rehearsal (2026-05-18) through indelible's chi router.
// Fixture data + capture context lives in testdata/scim/okta/README.md.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// oktaFixture mirrors the JSON shape produced by the mitmproxy extractor
// script (see commit history of the fixture branch for the script source).
type oktaFixture struct {
	Request struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	} `json:"request"`
	Response struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"response"`
}

func loadOktaFixture(t *testing.T, name string) oktaFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "scim", "okta", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var f oktaFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return f
}

// Per-run fields that vary regardless of correctness — strip from both sides
// before comparing actual vs expected response bodies.
var (
	idRE        = regexp.MustCompile(`"id"\s*:\s*"\d+"`)
	valueRE     = regexp.MustCompile(`"value"\s*:\s*"\d+"`)
	refRE       = regexp.MustCompile(`"(Users|Groups)/\d+"`)
	timestampRE = regexp.MustCompile(`"(created|lastModified)"\s*:\s*"[^"]+"`)
)

func normalizeSCIMResponse(raw []byte) string {
	s := string(raw)
	s = idRE.ReplaceAllString(s, `"id":"<ID>"`)
	s = valueRE.ReplaceAllString(s, `"value":"<ID>"`)
	s = refRE.ReplaceAllString(s, `"$1/<ID>"`)
	s = timestampRE.ReplaceAllString(s, `"$1":"<TIME>"`)
	return s
}

// jsonStructurallyEqual decides JSON equality after canonical re-encoding so
// field-order differences don't cause false fails.
func jsonStructurallyEqual(a, b string) bool {
	var aV, bV any
	if err := json.Unmarshal([]byte(a), &aV); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(b), &bV); err != nil {
		return false
	}
	aB, _ := json.Marshal(aV)
	bB, _ := json.Marshal(bV)
	return string(aB) == string(bB)
}

// --- Seed helpers --------------------------------------------------------

// seedUser creates one SCIM user with the given email. Returns nothing because
// internal IDs are auto-increment and predictable based on call order.
func seedUser(t *testing.T, env *scimTestEnv, email, given, family, externalID string) {
	t.Helper()
	body := map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName":   email,
		"externalId": externalID,
		"active":     true,
		"name":       map[string]string{"givenName": given, "familyName": family},
		"emails":     []map[string]any{{"value": email, "primary": true}},
	}
	w := env.do(t, "POST", "/scim/v2/Users", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed user %s: got %d, body: %s", email, w.Code, w.Body.String())
	}
}

func seedSentinels(t *testing.T, env *scimTestEnv, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		seedUser(t, env,
			fmt.Sprintf("sentinel%d@example.com", i),
			"Sentinel", fmt.Sprintf("%d", i), "")
	}
}

func seedFixtureUser(t *testing.T, env *scimTestEnv) {
	t.Helper()
	seedUser(t, env, "fixture.user@example.com", "fixture", "user", "00u13542539yHv3Xq698")
}

func deactivateUserID(t *testing.T, env *scimTestEnv, id int64) {
	t.Helper()
	patch := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{
			{"op": "replace", "value": map[string]bool{"active": false}},
		},
	}
	w := env.do(t, "PATCH", fmt.Sprintf("/scim/v2/Users/%d", id), patch)
	if w.Code != http.StatusOK {
		t.Fatalf("deactivate user %d: got %d, body: %s", id, w.Code, w.Body.String())
	}
}

func seedGroupWithMembers(t *testing.T, env *scimTestEnv, name string, memberIDs []int64) {
	t.Helper()
	members := make([]map[string]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		members = append(members, map[string]string{"value": fmt.Sprintf("%d", id)})
	}
	body := map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"displayName": name,
		"members":     members,
	}
	w := env.do(t, "POST", "/scim/v2/Groups", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed group %s: got %d, body: %s", name, w.Code, w.Body.String())
	}
}

// seedForFixture brings the DB to the state the captured fixture expects.
// Admin user from setupSCIMTest occupies id=1 in every env.
//
// Capture context: the rehearsal session created users at ids 1 (admin),
// 2 (Okta auto-provisioned), 3 (Test Provisioning), and 4 (Fixture User).
// Group id=1 (SCIMTestGroup) had member id=3 before each captured fixture.
func seedForFixture(t *testing.T, env *scimTestEnv, fixtureName string) {
	t.Helper()
	switch fixtureName {
	case "user_existence_check.json":
		// No prereq — fixture's expected response is an empty list. Existing admin
		// user has a different userName from the filter, so we'd get the same
		// empty-list response either way.
	case "create_user.json":
		// Capture POSTed fixture.user and got id=4. Seed 2 sentinels so the next
		// POST consumes id=4 (admin already has id=1).
		seedSentinels(t, env, 2)
	case "update_user.json", "deactivate_user.json":
		// Need user with id=4 to exist (target of PUT/PATCH).
		seedSentinels(t, env, 2)
		seedFixtureUser(t, env)
	case "reactivate_user.json":
		// Same as deactivate, then flip the user inactive so the captured PATCH
		// has something meaningful to reactivate.
		seedSentinels(t, env, 2)
		seedFixtureUser(t, env)
		deactivateUserID(t, env, 4)
	case "group_metadata_patch.json":
		// Group id=1 must exist with member id=3 (matches the captured response).
		seedSentinels(t, env, 2)
		seedGroupWithMembers(t, env, "SCIMTestGroup", []int64{3})
	case "group_add_member.json":
		// Group id=1 exists with member id=3. User id=4 exists. PATCH adds id=4.
		seedSentinels(t, env, 2)
		seedGroupWithMembers(t, env, "SCIMTestGroup", []int64{3})
		seedFixtureUser(t, env)
	case "group_remove_member.json":
		// Group id=1 exists with members 3 AND 4 (the PATCH removes id=4).
		seedSentinels(t, env, 2)
		seedFixtureUser(t, env)
		seedGroupWithMembers(t, env, "SCIMTestGroup", []int64{3, 4})
	}
}

// --- Table-driven replay -------------------------------------------------

func TestSCIM_OktaFixtures(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("testdata", "scim", "okta", "*.json"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no fixtures found under testdata/scim/okta/")
	}
	for _, path := range matches {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			env := setupSCIMTest(t)
			seedForFixture(t, env, name)
			f := loadOktaFixture(t, name)

			// Replay the captured request through the chi router.
			req := httptest.NewRequest(f.Request.Method, f.Request.Path, bytes.NewReader(f.Request.Body))
			req.Header.Set("Content-Type", scimContentType)
			req.Header.Set("Authorization", "Bearer "+env.scimSecret)
			w := httptest.NewRecorder()
			env.router.ServeHTTP(w, req)

			if w.Code != f.Response.Status {
				t.Fatalf("status: got %d, want %d\nrequest body: %s\nresponse body: %s",
					w.Code, f.Response.Status, string(f.Request.Body), w.Body.String())
			}

			// Compare normalized JSON. Per-run fields (timestamps, generated ids)
			// are replaced with placeholders on both sides.
			expected := normalizeSCIMResponse(f.Response.Body)
			actual := normalizeSCIMResponse(w.Body.Bytes())
			if !jsonStructurallyEqual(expected, actual) {
				t.Errorf("response body mismatch\n--- expected (normalized) ---\n%s\n--- actual (normalized) ---\n%s",
					expected, actual)
			}
		})
	}
}

// --- Dedicated case: password ignored (finding #1 from rehearsal) ---------

func TestSCIM_OktaFixture_PasswordIsIgnored(t *testing.T) {
	// Real Okta captures include `password: "5qBqoy5N"` in POST /Users payloads
	// (Okta generates an initial random password). Indelible must silently
	// discard it so SCIM-provisioned users can only authenticate via SSO, not
	// via local password login.

	env := setupSCIMTest(t)
	f := loadOktaFixture(t, "create_user.json")

	// Pull the password value from the captured request body for the login probe.
	var reqBody map[string]any
	if err := json.Unmarshal(f.Request.Body, &reqBody); err != nil {
		t.Fatalf("parse fixture body: %v", err)
	}
	capturedPassword, _ := reqBody["password"].(string)
	if capturedPassword == "" {
		t.Skip("fixture has no captured password — finding may no longer apply")
	}
	userName, _ := reqBody["userName"].(string)

	// Run the captured POST through indelible.
	w := env.do(t, "POST", "/scim/v2/Users", json.RawMessage(f.Request.Body))
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /Users: got %d, body: %s", w.Code, w.Body.String())
	}

	// Confirm the response omits any password reflection (SCIM spec requires this).
	if strings.Contains(w.Body.String(), capturedPassword) {
		t.Errorf("response body leaked the captured password value")
	}

	// Probe: attempt a local login as the new user with the captured password.
	// Expectation: 401 — indelible never persisted the password as a credential.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    userName,
		"password": capturedPassword,
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	env.router.ServeHTTP(lw, req)

	if lw.Code == http.StatusOK {
		t.Errorf("SCIM-provisioned user successfully logged in with Okta-provided password — "+
			"indelible must NOT consume the SCIM password field (got %d, body: %s)",
			lw.Code, lw.Body.String())
	}
}

// --- Dedicated case: metadata-only PATCH is a no-op (finding #2) ----------

func TestSCIM_OktaFixture_GroupMetadataPATCHIsNoop(t *testing.T) {
	// Okta sends a no-op `replace` of {id, displayName} before every real
	// add/remove membership operation. Indelible must tolerate it without
	// mutating the group state.

	env := setupSCIMTest(t)
	seedForFixture(t, env, "group_metadata_patch.json")

	// Snapshot the group before the no-op PATCH.
	preW := env.do(t, "GET", "/scim/v2/Groups/1", nil)
	if preW.Code != http.StatusOK {
		t.Fatalf("GET pre-state: %d, body: %s", preW.Code, preW.Body.String())
	}

	// Replay the captured metadata PATCH.
	f := loadOktaFixture(t, "group_metadata_patch.json")
	w := env.do(t, "PATCH", "/scim/v2/Groups/1", json.RawMessage(f.Request.Body))
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH metadata: %d, body: %s", w.Code, w.Body.String())
	}

	// Group state after should equal state before (modulo timestamps).
	postW := env.do(t, "GET", "/scim/v2/Groups/1", nil)
	if postW.Code != http.StatusOK {
		t.Fatalf("GET post-state: %d, body: %s", postW.Code, postW.Body.String())
	}
	pre := normalizeSCIMResponse(preW.Body.Bytes())
	post := normalizeSCIMResponse(postW.Body.Bytes())
	if !jsonStructurallyEqual(pre, post) {
		t.Errorf("group changed across no-op metadata PATCH\n--- before ---\n%s\n--- after ---\n%s", pre, post)
	}
}

// --- Dedicated case: filter-by-value path on remove (finding #3) ----------

func TestSCIM_OktaFixture_FilterByValueRemove(t *testing.T) {
	// Okta sends `op: remove, path: "members[value eq \"X\"]"` for membership
	// removes — the SCIM filter-by-value path. Tier 1 must keep this parsing
	// working since both Okta and Azure AD use this shape.

	env := setupSCIMTest(t)
	seedForFixture(t, env, "group_remove_member.json")

	// Before: group has 2 members (ids 3 and 4)
	preW := env.do(t, "GET", "/scim/v2/Groups/1", nil)
	if preW.Code != http.StatusOK {
		t.Fatalf("GET pre-state: %d, body: %s", preW.Code, preW.Body.String())
	}
	var preGroup map[string]any
	json.Unmarshal(preW.Body.Bytes(), &preGroup)
	preMembers, _ := preGroup["members"].([]any)
	if len(preMembers) != 2 {
		t.Fatalf("seed didn't produce 2 members, got %d (body: %s)", len(preMembers), preW.Body.String())
	}

	// Replay the captured remove (filter-by-value on member with value "4")
	f := loadOktaFixture(t, "group_remove_member.json")
	w := env.do(t, "PATCH", "/scim/v2/Groups/1", json.RawMessage(f.Request.Body))
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH remove: %d, body: %s", w.Code, w.Body.String())
	}

	// After: exactly one member should remain (id 3)
	postW := env.do(t, "GET", "/scim/v2/Groups/1", nil)
	if postW.Code != http.StatusOK {
		t.Fatalf("GET post-state: %d, body: %s", postW.Code, postW.Body.String())
	}
	var postGroup map[string]any
	json.Unmarshal(postW.Body.Bytes(), &postGroup)
	postMembers, _ := postGroup["members"].([]any)
	if len(postMembers) != 1 {
		t.Errorf("expected 1 remaining member after filter-by-value remove, got %d (body: %s)",
			len(postMembers), postW.Body.String())
	} else if m, ok := postMembers[0].(map[string]any); ok {
		if v, _ := m["value"].(string); v != "3" {
			t.Errorf("wrong member remained after remove: got value=%q, want \"3\"", v)
		}
	}
}
