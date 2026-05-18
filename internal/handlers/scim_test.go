package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func scimFilterURL(base, filter string) string {
	return base + "?filter=" + url.QueryEscape(filter)
}

// --- SCIM test helpers ---------------------------------------------------

const scimContentType = "application/scim+json"

// scimTestEnv bundles the router and a working SCIM bearer token so each
// test starts from the same enabled-and-tokened baseline.
type scimTestEnv struct {
	router     http.Handler
	adminToken string
	scimSecret string // raw "scim_<hex>" secret
	scimTokID  int64
}

func setupSCIMTest(t *testing.T) *scimTestEnv {
	t.Helper()
	router := setupTestRouter(t)
	adminToken := registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	// Enable SCIM via the admin settings API (mirrors what a real operator does).
	patchBody, _ := json.Marshal(map[string]string{"scim_enabled": "true"})
	req := httptest.NewRequest("PATCH", "/api/v2/admin/settings", bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable scim: got %d, body: %s", w.Code, w.Body.String())
	}

	// Mint a SCIM token.
	tokenBody, _ := json.Marshal(map[string]string{"name": "test-okta"})
	req = httptest.NewRequest("POST", "/api/v2/admin/scim/tokens", bytes.NewReader(tokenBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create scim token: got %d, body: %s", w.Code, w.Body.String())
	}
	var tokResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tokResp); err != nil {
		t.Fatalf("decode token resp: %v", err)
	}
	tokMeta := tokResp["token"].(map[string]any)
	return &scimTestEnv{
		router:     router,
		adminToken: adminToken,
		scimSecret: tokResp["secret"].(string),
		scimTokID:  int64(tokMeta["id"].(float64)),
	}
}

func (e *scimTestEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", scimContentType)
	req.Header.Set("Authorization", "Bearer "+e.scimSecret)
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, w.Body.String())
	}
	return out
}

func scimUserBody(email, given, family, externalID string, active bool) map[string]any {
	return map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName":   email,
		"externalId": externalID,
		"active":     active,
		"name":       map[string]string{"givenName": given, "familyName": family},
		"emails": []map[string]any{
			{"value": email, "primary": true},
		},
	}
}

func scimGroupBody(displayName, externalID string, memberIDs ...int64) map[string]any {
	members := make([]map[string]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		members = append(members, map[string]string{"value": strconv.FormatInt(id, 10)})
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"displayName": displayName,
		"externalId":  externalID,
		"members":     members,
	}
}

func scimPatchBody(ops ...map[string]any) map[string]any {
	return map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": ops,
	}
}

// --- Discovery -----------------------------------------------------------

func TestSCIM_Discovery(t *testing.T) {
	env := setupSCIMTest(t)

	cases := []struct {
		path    string
		wantKey string
	}{
		{"/scim/v2/ServiceProviderConfig", "patch"},
		{"/scim/v2/Schemas", "Resources"},
		{"/scim/v2/ResourceTypes", "Resources"},
	}
	for _, tc := range cases {
		w := env.do(t, "GET", tc.path, nil)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: got %d, body: %s", tc.path, w.Code, w.Body.String())
			continue
		}
		if ct := w.Header().Get("Content-Type"); ct != scimContentType {
			t.Errorf("GET %s: Content-Type = %q, want %q", tc.path, ct, scimContentType)
		}
		body := decodeJSON(t, w)
		if _, ok := body[tc.wantKey]; !ok {
			t.Errorf("GET %s: missing key %q in response", tc.path, tc.wantKey)
		}
	}
}

func TestSCIM_ResourceTypes_HasUserAndGroup(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "GET", "/scim/v2/ResourceTypes", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	resources, ok := body["Resources"].([]any)
	if !ok {
		t.Fatalf("Resources not an array")
	}
	names := map[string]bool{}
	for _, r := range resources {
		if m, ok := r.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names[name] = true
			}
		}
	}
	if !names["User"] || !names["Group"] {
		t.Errorf("expected both User and Group resource types, got %v", names)
	}
}

// --- Auth failures -------------------------------------------------------

func TestSCIM_Auth_Disabled(t *testing.T) {
	// Don't enable scim_enabled — middleware should 404.
	router := setupTestRouter(t)
	registerAndGetToken(t, router, "admin@test.com", "password123", "Admin", "User")

	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Bearer scim_anything")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("disabled SCIM should 404, got %d, body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != scimContentType {
		t.Errorf("Content-Type = %q, want %q", ct, scimContentType)
	}
}

func TestSCIM_Auth_MissingHeader(t *testing.T) {
	env := setupSCIMTest(t)
	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	// Deliberately no Authorization header.
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing header should 401, got %d", w.Code)
	}
}

func TestSCIM_Auth_WrongPrefix(t *testing.T) {
	env := setupSCIMTest(t)
	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Token "+env.scimSecret) // wrong scheme
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong auth prefix should 401, got %d", w.Code)
	}
}

func TestSCIM_Auth_InvalidToken(t *testing.T) {
	env := setupSCIMTest(t)
	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Bearer scim_doesnotexist")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unknown token should 401, got %d", w.Code)
	}
}

func TestSCIM_Auth_RevokedToken(t *testing.T) {
	env := setupSCIMTest(t)

	// Revoke the token via admin API.
	req := httptest.NewRequest("DELETE",
		fmt.Sprintf("/api/v2/admin/scim/tokens/%d", env.scimTokID), nil)
	req.Header.Set("Authorization", "Bearer "+env.adminToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: got %d, body: %s", w.Code, w.Body.String())
	}

	// Now the SCIM secret should be rejected.
	w = env.do(t, "GET", "/scim/v2/Users", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("revoked token should 401, got %d", w.Code)
	}
}

// --- Users ---------------------------------------------------------------

func TestSCIM_User_Create(t *testing.T) {
	env := setupSCIMTest(t)
	body := scimUserBody("alice@test.com", "Alice", "Smith", "okta-abc-123", true)
	w := env.do(t, "POST", "/scim/v2/Users", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["id"] == nil {
		t.Fatal("response missing id")
	}
	if resp["userName"] != "alice@test.com" {
		t.Errorf("userName = %v", resp["userName"])
	}
	if resp["externalId"] != "okta-abc-123" {
		t.Errorf("externalId = %v", resp["externalId"])
	}
	if resp["active"] != true {
		t.Errorf("active = %v", resp["active"])
	}
	if name, ok := resp["name"].(map[string]any); !ok || name["givenName"] != "Alice" || name["familyName"] != "Smith" {
		t.Errorf("name = %v", resp["name"])
	}
}

func TestSCIM_User_Get(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("bob@test.com", "Bob", "Jones", "okta-bob", true))
	id := decodeJSON(t, w)["id"].(string)

	w = env.do(t, "GET", "/scim/v2/Users/"+id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["userName"] != "bob@test.com" {
		t.Errorf("userName = %v", resp["userName"])
	}
}

func TestSCIM_User_Get_NotFound(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "GET", "/scim/v2/Users/9999999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("get missing: got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSCIM_User_FilterByUserName(t *testing.T) {
	env := setupSCIMTest(t)
	env.do(t, "POST", "/scim/v2/Users", scimUserBody("carol@test.com", "Carol", "Lin", "okta-carol", true))
	env.do(t, "POST", "/scim/v2/Users", scimUserBody("dave@test.com", "Dave", "Park", "okta-dave", true))

	// Azure AD sends this exact shape before every POST as an existence check.
	w := env.do(t, "GET", scimFilterURL("/scim/v2/Users", `userName eq "carol@test.com"`), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("filter: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if int(resp["totalResults"].(float64)) != 1 {
		t.Errorf("totalResults = %v, want 1", resp["totalResults"])
	}
	resources := resp["Resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("Resources len = %d, want 1", len(resources))
	}
	if resources[0].(map[string]any)["userName"] != "carol@test.com" {
		t.Errorf("filtered userName = %v", resources[0])
	}

	// No match → empty list, still 200.
	w = env.do(t, "GET", scimFilterURL("/scim/v2/Users", `userName eq "ghost@test.com"`), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("empty filter: got %d", w.Code)
	}
	resp = decodeJSON(t, w)
	if int(resp["totalResults"].(float64)) != 0 {
		t.Errorf("empty filter totalResults = %v, want 0", resp["totalResults"])
	}
}

func TestSCIM_User_List_Paginated(t *testing.T) {
	env := setupSCIMTest(t)
	for i := 0; i < 3; i++ {
		email := fmt.Sprintf("user%d@test.com", i)
		env.do(t, "POST", "/scim/v2/Users", scimUserBody(email, "User", strconv.Itoa(i), "ext-"+strconv.Itoa(i), true))
	}

	w := env.do(t, "GET", "/scim/v2/Users", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	// Admin user + 3 created = 4.
	if int(resp["totalResults"].(float64)) != 4 {
		t.Errorf("totalResults = %v, want 4", resp["totalResults"])
	}
}

func TestSCIM_User_Replace(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("eve@test.com", "Eve", "Old", "okta-eve", true))
	id := decodeJSON(t, w)["id"].(string)

	replaced := scimUserBody("eve@test.com", "Eve", "New", "okta-eve", true)
	w = env.do(t, "PUT", "/scim/v2/Users/"+id, replaced)
	if w.Code != http.StatusOK {
		t.Fatalf("put: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	name := resp["name"].(map[string]any)
	if name["familyName"] != "New" {
		t.Errorf("familyName = %v, want New", name["familyName"])
	}
}

func TestSCIM_User_Patch_Active(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("frank@test.com", "Frank", "Z", "okta-frank", true))
	id := decodeJSON(t, w)["id"].(string)

	// Azure AD-style PATCH: replace active=false (soft deactivate).
	patch := scimPatchBody(map[string]any{
		"op": "replace", "path": "active", "value": false,
	})
	w = env.do(t, "PATCH", "/scim/v2/Users/"+id, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: got %d, body: %s", w.Code, w.Body.String())
	}
	if decodeJSON(t, w)["active"] != false {
		t.Errorf("active should be false after patch")
	}
}

func TestSCIM_User_Patch_OktaCapitalizedOp(t *testing.T) {
	// Okta sometimes serializes the `op` value with a capital letter ("Replace")
	// where the spec says lowercase. Verify we tolerate both shapes.
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("grace@test.com", "Grace", "Hopper", "okta-grace", true))
	id := decodeJSON(t, w)["id"].(string)

	patch := scimPatchBody(map[string]any{
		"op": "Replace", "path": "name.givenName", "value": "Grazia",
	})
	w = env.do(t, "PATCH", "/scim/v2/Users/"+id, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("capitalized Replace patch: got %d, body: %s", w.Code, w.Body.String())
	}
	name := decodeJSON(t, w)["name"].(map[string]any)
	if name["givenName"] != "Grazia" {
		t.Errorf("givenName = %v, want Grazia", name["givenName"])
	}
}

func TestSCIM_User_Delete(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("hank@test.com", "Hank", "Q", "okta-hank", true))
	id := decodeJSON(t, w)["id"].(string)

	w = env.do(t, "DELETE", "/scim/v2/Users/"+id, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d, body: %s", w.Code, w.Body.String())
	}

	// Subsequent GET should 404 (soft-deleted users are filtered out by GetByID).
	w = env.do(t, "GET", "/scim/v2/Users/"+id, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", w.Code)
	}
}

func TestSCIM_User_Lifecycle(t *testing.T) {
	// Full create → update → deactivate → reactivate cycle (Okta provisioning test).
	env := setupSCIMTest(t)

	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("life@test.com", "Life", "Cycle", "okta-life", true))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, body: %s", w.Code, w.Body.String())
	}
	id := decodeJSON(t, w)["id"].(string)

	// Update name via PATCH.
	patch := scimPatchBody(map[string]any{
		"op": "replace", "path": "name.familyName", "value": "Renamed",
	})
	w = env.do(t, "PATCH", "/scim/v2/Users/"+id, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: got %d, body: %s", w.Code, w.Body.String())
	}

	// Deactivate.
	w = env.do(t, "PATCH", "/scim/v2/Users/"+id, scimPatchBody(map[string]any{
		"op": "replace", "path": "active", "value": false,
	}))
	if w.Code != http.StatusOK || decodeJSON(t, w)["active"] != false {
		t.Fatalf("deactivate: got %d, body: %s", w.Code, w.Body.String())
	}

	// Reactivate.
	w = env.do(t, "PATCH", "/scim/v2/Users/"+id, scimPatchBody(map[string]any{
		"op": "replace", "path": "active", "value": true,
	}))
	if w.Code != http.StatusOK || decodeJSON(t, w)["active"] != true {
		t.Fatalf("reactivate: got %d, body: %s", w.Code, w.Body.String())
	}
}

// --- Groups --------------------------------------------------------------

func TestSCIM_Group_Create(t *testing.T) {
	env := setupSCIMTest(t)
	// Create a member first.
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("ivan@test.com", "Ivan", "K", "okta-ivan", true))
	uid := decodeJSON(t, w)["id"].(string)
	uidInt, _ := strconv.ParseInt(uid, 10, 64)

	w = env.do(t, "POST", "/scim/v2/Groups", scimGroupBody("Engineering", "okta-grp-eng", uidInt))
	if w.Code != http.StatusCreated {
		t.Fatalf("create group: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["displayName"] != "Engineering" {
		t.Errorf("displayName = %v", resp["displayName"])
	}
	if resp["externalId"] != "okta-grp-eng" {
		t.Errorf("externalId = %v", resp["externalId"])
	}
	members := resp["members"].([]any)
	if len(members) != 1 {
		t.Errorf("members len = %d, want 1", len(members))
	}
}

func TestSCIM_Group_Patch_AddRemoveMember(t *testing.T) {
	env := setupSCIMTest(t)

	// Two users + a group with the first as a member.
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("jack@test.com", "Jack", "A", "okta-jack", true))
	uid1, _ := strconv.ParseInt(decodeJSON(t, w)["id"].(string), 10, 64)
	w = env.do(t, "POST", "/scim/v2/Users", scimUserBody("kate@test.com", "Kate", "B", "okta-kate", true))
	uid2Str := decodeJSON(t, w)["id"].(string)
	uid2, _ := strconv.ParseInt(uid2Str, 10, 64)

	w = env.do(t, "POST", "/scim/v2/Groups", scimGroupBody("Sales", "okta-grp-sales", uid1))
	gid := decodeJSON(t, w)["id"].(string)

	// Add kate via PATCH.
	patch := scimPatchBody(map[string]any{
		"op":    "add",
		"path":  "members",
		"value": []map[string]string{{"value": strconv.FormatInt(uid2, 10)}},
	})
	w = env.do(t, "PATCH", "/scim/v2/Groups/"+gid, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("add member: got %d, body: %s", w.Code, w.Body.String())
	}
	if len(decodeJSON(t, w)["members"].([]any)) != 2 {
		t.Errorf("group should have 2 members after add")
	}

	// Remove kate via Azure-style filtered path: members[value eq "uid2"].
	patch = scimPatchBody(map[string]any{
		"op":   "remove",
		"path": fmt.Sprintf(`members[value eq "%s"]`, uid2Str),
	})
	w = env.do(t, "PATCH", "/scim/v2/Groups/"+gid, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("remove member: got %d, body: %s", w.Code, w.Body.String())
	}
	if len(decodeJSON(t, w)["members"].([]any)) != 1 {
		t.Errorf("group should have 1 member after remove")
	}
}

func TestSCIM_Group_Replace_AtomicMembers(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Users", scimUserBody("leo@test.com", "Leo", "C", "okta-leo", true))
	uid1, _ := strconv.ParseInt(decodeJSON(t, w)["id"].(string), 10, 64)
	w = env.do(t, "POST", "/scim/v2/Users", scimUserBody("mia@test.com", "Mia", "D", "okta-mia", true))
	uid2, _ := strconv.ParseInt(decodeJSON(t, w)["id"].(string), 10, 64)

	w = env.do(t, "POST", "/scim/v2/Groups", scimGroupBody("Marketing", "okta-grp-mkt", uid1))
	gid := decodeJSON(t, w)["id"].(string)

	// PUT replaces the whole resource — including atomically swapping members.
	w = env.do(t, "PUT", "/scim/v2/Groups/"+gid, scimGroupBody("Marketing", "okta-grp-mkt", uid2))
	if w.Code != http.StatusOK {
		t.Fatalf("put: got %d, body: %s", w.Code, w.Body.String())
	}
	members := decodeJSON(t, w)["members"].([]any)
	if len(members) != 1 {
		t.Fatalf("members len = %d, want 1", len(members))
	}
	if members[0].(map[string]any)["value"] != strconv.FormatInt(uid2, 10) {
		t.Errorf("after replace, only uid2 should remain; got %v", members[0])
	}
}

func TestSCIM_Group_Delete(t *testing.T) {
	env := setupSCIMTest(t)
	w := env.do(t, "POST", "/scim/v2/Groups", scimGroupBody("Ops", "okta-grp-ops"))
	gid := decodeJSON(t, w)["id"].(string)

	w = env.do(t, "DELETE", "/scim/v2/Groups/"+gid, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d, body: %s", w.Code, w.Body.String())
	}

	w = env.do(t, "GET", "/scim/v2/Groups/"+gid, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", w.Code)
	}
}

func TestSCIM_Group_List(t *testing.T) {
	env := setupSCIMTest(t)
	env.do(t, "POST", "/scim/v2/Groups", scimGroupBody("G1", "ext-g1"))
	env.do(t, "POST", "/scim/v2/Groups", scimGroupBody("G2", "ext-g2"))

	w := env.do(t, "GET", "/scim/v2/Groups", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, body: %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if int(resp["totalResults"].(float64)) != 2 {
		t.Errorf("totalResults = %v, want 2", resp["totalResults"])
	}
}
