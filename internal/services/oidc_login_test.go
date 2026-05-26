package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// --- Cookie round-trip ------------------------------------------------------

const testCookieKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestOIDCCookieEncodeDecode_RoundTrip(t *testing.T) {
	original := oidcStatePayload{
		ProviderID:   42,
		State:        "abcdef",
		Nonce:        "uvwxyz",
		CodeVerifier: "verifier-value",
		RedirectURL:  "https://example.com/cb",
		LinkToUserID: 7,
		Exp:          time.Now().Add(10 * time.Minute).Unix(),
	}
	cookie, err := encodeOIDCStateCookie(testCookieKey, original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeOIDCStateCookie(testCookieKey, cookie)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != original {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, original)
	}
}

func TestOIDCCookieDecode_RejectsTampered(t *testing.T) {
	cookie, _ := encodeOIDCStateCookie(testCookieKey, oidcStatePayload{ProviderID: 1, Exp: time.Now().Unix() + 60})
	// Flip a byte mid-cookie — AES-GCM auth tag should reject.
	bad := cookie[:len(cookie)-3] + "Aa_"
	if _, err := decodeOIDCStateCookie(testCookieKey, bad); err == nil {
		t.Error("tampered cookie should not decode")
	}
}

func TestOIDCCookieDecode_RejectsWrongKey(t *testing.T) {
	cookie, _ := encodeOIDCStateCookie(testCookieKey, oidcStatePayload{ProviderID: 1, Exp: time.Now().Unix() + 60})
	other := "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := decodeOIDCStateCookie(other, cookie); err == nil {
		t.Error("decoding with wrong key should fail")
	}
}

// --- splitScopes / splitName ------------------------------------------------

func TestSplitScopes(t *testing.T) {
	cases := map[string][]string{
		"":                                   {"openid", "email", "profile"},
		"openid,email,profile":               {"openid", "email", "profile"},
		"email, profile":                     {"openid", "email", "profile"}, // openid auto-prepended
		"openid,email,profile,offline_access": {"openid", "email", "profile", "offline_access"},
	}
	for in, want := range cases {
		got := splitScopes(in)
		if !equalStrings(got, want) {
			t.Errorf("splitScopes(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct {
		given, family, full string
		wantFirst, wantLast string
	}{
		{"Alice", "Wonder", "Alice Wonder", "Alice", "Wonder"},
		{"", "", "Bob Builder", "Bob", "Builder"},
		{"", "", "Cher", "Cher", ""},
		{"", "Lin", "", "", "Lin"},
		{"", "", "", "", ""},
	}
	for _, c := range cases {
		f, l := splitName(c.given, c.family, c.full)
		if f != c.wantFirst || l != c.wantLast {
			t.Errorf("splitName(%q,%q,%q) = (%q,%q), want (%q,%q)",
				c.given, c.family, c.full, f, l, c.wantFirst, c.wantLast)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- LinkIdentity / UnlinkIdentity ------------------------------------------

func setupOIDCFixture(t *testing.T) (*OIDCLoginService, *OIDCProviderService, int64, int64) {
	t.Helper()
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	provider, err := providerSvc.Create("okta", "Okta", "https://issuer.example.com", "client-id", "client-secret", "openid,email,profile")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := createTestUser(t, NewUserService(db), "alice@example.com", "Alice", "L")
	return loginSvc, providerSvc, provider.ID, user.ID
}

func TestLinkIdentity_Idempotent(t *testing.T) {
	loginSvc, _, providerID, userID := setupOIDCFixture(t)
	if err := loginSvc.LinkIdentity(userID, providerID, "okta-abc"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if err := loginSvc.LinkIdentity(userID, providerID, "okta-abc"); err != nil {
		t.Fatalf("idempotent link: %v", err)
	}
	ids, _ := loginSvc.ListIdentitiesForUser(userID)
	if len(ids) != 1 {
		t.Errorf("expected 1 identity after idempotent link, got %d", len(ids))
	}
}

func TestLinkIdentity_RejectsDifferentUser(t *testing.T) {
	loginSvc, _, providerID, userID := setupOIDCFixture(t)
	otherUser := createTestUser(t, NewUserService(loginSvc.db), "bob@example.com", "Bob", "Q")

	if err := loginSvc.LinkIdentity(userID, providerID, "okta-shared"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	err := loginSvc.LinkIdentity(otherUser.ID, providerID, "okta-shared")
	if err != ErrOIDCIdentityLinkedOther {
		t.Errorf("expected ErrOIDCIdentityLinkedOther, got %v", err)
	}
}

func TestUnlinkIdentity_RejectsWhenLastLoginMethod(t *testing.T) {
	loginSvc, _, providerID, _ := setupOIDCFixture(t)
	// Make a SCIM-style user (no password) so we exercise the "no password
	// AND only one identity" guard.
	noPwUser, err := NewUserService(loginSvc.db).CreateFromSCIM("scim@example.com", "S", "U", "ext-1")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}
	if err := loginSvc.LinkIdentity(noPwUser.ID, providerID, "okta-only"); err != nil {
		t.Fatalf("link: %v", err)
	}
	ids, _ := loginSvc.ListIdentitiesForUser(noPwUser.ID)
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}

	err = loginSvc.UnlinkIdentity(ids[0].ID, noPwUser.ID)
	if err != ErrOIDCCannotUnlinkLast {
		t.Errorf("expected ErrOIDCCannotUnlinkLast, got %v", err)
	}
}

func TestUnlinkIdentity_AllowsWhenPasswordExists(t *testing.T) {
	loginSvc, _, providerID, userID := setupOIDCFixture(t)
	if err := loginSvc.LinkIdentity(userID, providerID, "okta-1"); err != nil {
		t.Fatalf("link: %v", err)
	}
	ids, _ := loginSvc.ListIdentitiesForUser(userID)
	if err := loginSvc.UnlinkIdentity(ids[0].ID, userID); err != nil {
		t.Errorf("unlink should succeed (user has password), got %v", err)
	}
}

func TestUnlinkIdentity_AllowsWhenMultipleIdentities(t *testing.T) {
	loginSvc, providerSvc, providerID1, _ := setupOIDCFixture(t)
	provider2, _ := providerSvc.Create("ad", "Azure AD", "https://ad.example.com", "cid2", "secret2", "openid,email,profile")

	noPwUser, _ := NewUserService(loginSvc.db).CreateFromSCIM("scim@example.com", "S", "U", "ext-2")
	if err := loginSvc.LinkIdentity(noPwUser.ID, providerID1, "okta-1"); err != nil {
		t.Fatalf("link 1: %v", err)
	}
	if err := loginSvc.LinkIdentity(noPwUser.ID, provider2.ID, "ad-1"); err != nil {
		t.Fatalf("link 2: %v", err)
	}
	ids, _ := loginSvc.ListIdentitiesForUser(noPwUser.ID)
	if len(ids) != 2 {
		t.Fatalf("expected 2 identities, got %d", len(ids))
	}
	if err := loginSvc.UnlinkIdentity(ids[0].ID, noPwUser.ID); err != nil {
		t.Errorf("unlink should succeed (2 identities exist), got %v", err)
	}
}

// --- BuildAuthorizeURL: ExtraAuthorizeParams appended -----------------------

func TestBuildAuthorizeURL_AppendsExtraParams(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)

	idp := newFakeIDP(t, "test-client")
	provider, err := providerSvc.Create("google", "Google", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	// Google Workspace domain restriction — the launch-blocking case behind V2-313.
	if err := providerSvc.SetExtraAuthorizeParams(provider.ID, map[string]string{
		"hd":     "company.com",
		"prompt": "select_account",
	}); err != nil {
		t.Fatalf("SetExtraAuthorizeParams: %v", err)
	}

	authURL, _, err := loginSvc.BuildAuthorizeURL(context.Background(), provider.ID, AuthorizeOpts{
		RedirectURL: idp.server.URL + "/cb",
	})
	if err != nil {
		t.Fatalf("BuildAuthorizeURL: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := u.Query()
	if got := q.Get("hd"); got != "company.com" {
		t.Errorf("hd = %q, want company.com (full URL: %s)", got, authURL)
	}
	if got := q.Get("prompt"); got != "select_account" {
		t.Errorf("prompt = %q, want select_account", got)
	}
	// SDK-managed params must survive intact alongside the extras.
	if q.Get("state") == "" {
		t.Error("state missing from authorize URL")
	}
	if q.Get("nonce") == "" {
		t.Error("nonce missing from authorize URL")
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge missing from authorize URL (PKCE broken)")
	}
}

func TestBuildAuthorizeURL_NoExtraParamsByDefault(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("okta", "Okta", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	authURL, _, err := loginSvc.BuildAuthorizeURL(context.Background(), provider.ID, AuthorizeOpts{
		RedirectURL: idp.server.URL + "/cb",
	})
	if err != nil {
		t.Fatalf("BuildAuthorizeURL: %v", err)
	}
	u, _ := url.Parse(authURL)
	if u.Query().Get("hd") != "" {
		t.Errorf("hd should be absent when no extras configured, got %q", u.Query().Get("hd"))
	}
}

func TestUnlinkIdentity_RejectsForeignIdentity(t *testing.T) {
	loginSvc, _, providerID, userID := setupOIDCFixture(t)
	otherUser := createTestUser(t, NewUserService(loginSvc.db), "bob@example.com", "Bob", "Q")
	if err := loginSvc.LinkIdentity(otherUser.ID, providerID, "okta-bob"); err != nil {
		t.Fatalf("link: %v", err)
	}
	ids, _ := loginSvc.ListIdentitiesForUser(otherUser.ID)
	if err := loginSvc.UnlinkIdentity(ids[0].ID, userID); err == nil {
		t.Error("expected error unlinking someone else's identity")
	}
}

// --- HandleCallback against a fake IdP --------------------------------------
//
// The fake IdP serves the OIDC discovery doc + JWKS + token endpoint with an
// in-memory RSA key, so we can exercise the full discovery → exchange →
// verify pipeline without an external dependency. Chunk D adds Dex on top of
// this for the handler+browser integration test.

type fakeIDP struct {
	t          *testing.T
	server     *httptest.Server
	key        *rsa.PrivateKey
	jwks       string
	clientID   string
	authCode   string
	nextSub    string
	nextEmail  string
	nextEV     bool
	nextNonce  string
	codeChecks []func(req *http.Request)
}

func newFakeIDP(t *testing.T, clientID string) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	idp := &fakeIDP{t: t, key: key, clientID: clientID, authCode: "fake-auth-code", nextSub: "sub-1", nextEmail: "user@example.com", nextEV: true}

	// Precompute JWKS.
	jwk := jose.JSONWebKey{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig", KeyID: "test-key-1"}
	jwks, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	idp.jwks = string(jwks)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := idp.server.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(idp.jwks))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		for _, c := range idp.codeChecks {
			c(r)
		}
		idToken := idp.signIDToken(idp.nextSub, idp.nextEmail, idp.nextEV, idp.nextNonce, "Given", "Family", "Given Family")
		// oauth2 client requires explicit Content-Type to JSON-decode.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access",
			"token_type":   "Bearer",
			"id_token":     idToken,
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		// Not exercised by HandleCallback tests — we drive the callback directly.
		http.Error(w, "authorize endpoint not used in tests", http.StatusNotFound)
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIDP) signIDToken(sub, email string, emailVerified bool, nonce, given, family, name string) string {
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key-1"))
	if err != nil {
		f.t.Fatalf("signer: %v", err)
	}
	claims := map[string]any{
		"iss":            f.server.URL,
		"sub":            sub,
		"aud":            f.clientID,
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
		"nonce":          nonce,
		"email":          email,
		"email_verified": emailVerified,
		"given_name":     given,
		"family_name":    family,
		"name":           name,
	}
	raw, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		f.t.Fatalf("sign: %v", err)
	}
	return raw
}

// idpClientHelper builds an authorize URL and a matching cookie + state for
// HandleCallback, plus tracks the nonce so the fake IdP can sign it in.
func idpClientHelper(t *testing.T, svc *OIDCLoginService, providerID int64, redirect string, linkToUserID int64) (state, cookie, nonce, code string) {
	t.Helper()
	authURL, cookieValue, err := svc.BuildAuthorizeURL(context.Background(), providerID, AuthorizeOpts{
		RedirectURL: redirect, LinkToUserID: linkToUserID,
	})
	if err != nil {
		t.Fatalf("BuildAuthorizeURL: %v", err)
	}
	// Pluck `state` and `nonce` out of the authorize URL so the test (and the
	// fake IdP) can sign matching values without parsing the cookie.
	const stateMarker = "state="
	const nonceMarker = "nonce="
	state = paramFromURL(authURL, stateMarker)
	nonce = paramFromURL(authURL, nonceMarker)
	if state == "" || nonce == "" {
		t.Fatalf("authorize URL missing state/nonce: %s", authURL)
	}
	return state, cookieValue, nonce, "fake-auth-code"
}

func paramFromURL(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	i += len(marker)
	j := strings.IndexAny(s[i:], "&#")
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}

func TestHandleCallback_LoginWithExistingIdentity(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)

	idp := newFakeIDP(t, "test-client")
	provider, err := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Pre-link an identity for an existing user.
	user := createTestUser(t, NewUserService(db), "alice@example.com", "Alice", "L")
	if err := loginSvc.LinkIdentity(user.ID, provider.ID, "alice-sub"); err != nil {
		t.Fatalf("LinkIdentity: %v", err)
	}

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "alice-sub"
	idp.nextEmail = "alice@example.com"
	idp.nextNonce = nonce

	outcome, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if outcome.LoggedInUser == nil || outcome.LoggedInUser.ID != user.ID {
		t.Errorf("expected logged-in user %d, got %+v", user.ID, outcome)
	}
	if outcome.IsNewUser {
		t.Error("existing-identity login should not flag IsNewUser")
	}
}

func TestHandleCallback_AutoLinksSCIMUserByExternalID(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	// SCIM-provisioned user — sub matches external_id, no oidc_identities row yet.
	scimUser, err := NewUserService(db).CreateFromSCIM("scim@example.com", "Sc", "Im", "okta-scim-99")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "okta-scim-99"
	idp.nextEmail = "scim@example.com"
	idp.nextNonce = nonce

	outcome, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if outcome.LoggedInUser == nil || outcome.LoggedInUser.ID != scimUser.ID {
		t.Errorf("expected SCIM user to be logged in, got %+v", outcome)
	}

	// Identity row should now exist.
	ids, _ := loginSvc.ListIdentitiesForUser(scimUser.ID)
	if len(ids) != 1 || ids[0].Subject != "okta-scim-99" {
		t.Errorf("expected auto-linked identity, got %v", ids)
	}
}

func TestHandleCallback_NoAccountWhenAutoProvisionOff(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "unknown-sub"
	idp.nextEmail = "stranger@example.com"
	idp.nextNonce = nonce

	_, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != ErrOIDCNoAccount {
		t.Errorf("expected ErrOIDCNoAccount, got %v", err)
	}
}

func TestHandleCallback_EmailCollisionWhenAutoProvisionOn(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err := providerSvc.SetAutoProvision(provider.ID, true, 0); err != nil {
		t.Fatalf("SetAutoProvision: %v", err)
	}

	// Local user already has this email, no external_id correlation.
	createTestUser(t, NewUserService(db), "collide@example.com", "Co", "Llide")

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "new-sub"
	idp.nextEmail = "collide@example.com"
	idp.nextNonce = nonce

	_, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != ErrOIDCEmailCollision {
		t.Errorf("expected ErrOIDCEmailCollision, got %v", err)
	}
}

func TestHandleCallback_AutoProvisionsNewUserWithDefaultGroup(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	groupSvc := NewGroupService(db)

	g, err := groupSvc.Create("eng", "", "read")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err := providerSvc.SetAutoProvision(provider.ID, true, g.ID); err != nil {
		t.Fatalf("SetAutoProvision: %v", err)
	}

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "brand-new-sub"
	idp.nextEmail = "newuser@example.com"
	idp.nextNonce = nonce

	outcome, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if outcome.LoggedInUser == nil {
		t.Fatalf("expected user, got %+v", outcome)
	}
	if !outcome.IsNewUser {
		t.Error("expected IsNewUser=true on auto-provision")
	}
	members, _ := groupSvc.ListMembers(g.ID)
	found := false
	for _, m := range members {
		if m == outcome.LoggedInUser.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("auto-provisioned user not in default group; members=%v", members)
	}
}

func TestHandleCallback_RequireEmailVerified_StrictRejectsMissingClaim(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)

	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err := providerSvc.SetAutoProvision(provider.ID, true, 0); err != nil {
		t.Fatalf("SetAutoProvision: %v", err)
	}
	// require_email_verified defaults to true; sanity-check, then drive an
	// id_token that omits the claim (modeled here as emailVerified=false).
	if p, _ := providerSvc.GetByID(provider.ID); !p.RequireEmailVerified {
		t.Fatalf("expected default require_email_verified=true")
	}

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "sub-no-ev"
	idp.nextEmail = "user@example.com"
	idp.nextEV = false
	idp.nextNonce = nonce

	_, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if !errors.Is(err, ErrOIDCMissingEmail) {
		t.Errorf("expected ErrOIDCMissingEmail, got %v", err)
	}
}

func TestHandleCallback_RequireEmailVerified_LooseAcceptsMissingClaim(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)

	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err := providerSvc.SetAutoProvision(provider.ID, true, 0); err != nil {
		t.Fatalf("SetAutoProvision: %v", err)
	}
	if err := providerSvc.SetRequireEmailVerified(provider.ID, false); err != nil {
		t.Fatalf("SetRequireEmailVerified: %v", err)
	}

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	idp.nextSub = "sub-loose"
	idp.nextEmail = "loose@example.com"
	idp.nextEV = false
	idp.nextNonce = nonce

	outcome, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != nil {
		t.Fatalf("HandleCallback with loose verification: %v", err)
	}
	if outcome.LoggedInUser == nil {
		t.Fatal("expected provisioned user with require_email_verified=false")
	}
	if !outcome.IsNewUser {
		t.Error("expected IsNewUser=true on auto-provision")
	}
	if outcome.LoggedInUser.Email != "loose@example.com" {
		t.Errorf("provisioned user email = %q, want loose@example.com", outcome.LoggedInUser.Email)
	}
}

func TestHandleCallback_StateMismatch(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	_, cookie, _, _ := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)

	_, err := loginSvc.HandleCallback(context.Background(), cookie, "wrong-state", "fake-auth-code")
	if err != ErrOIDCStateMismatch {
		t.Errorf("expected ErrOIDCStateMismatch, got %v", err)
	}
}

func TestHandleCallback_ExpiredCookie(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	// Mint the cookie with an old clock so it's already expired by call time.
	loginSvc.now = func() time.Time { return time.Now().Add(-1 * time.Hour) }
	state, cookie, _, _ := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	loginSvc.now = time.Now // back to real time

	_, err := loginSvc.HandleCallback(context.Background(), cookie, state, "fake-auth-code")
	if err != ErrOIDCStateExpired {
		t.Errorf("expected ErrOIDCStateExpired, got %v", err)
	}
}

func TestHandleCallback_NonceMismatch(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	state, cookie, _, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", 0)
	// Deliberately sign a different nonce than the one in the cookie.
	idp.nextNonce = "not-the-real-nonce"
	idp.nextSub = "alice-sub"
	idp.nextEmail = "alice@example.com"

	_, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Errorf("expected nonce mismatch error, got %v", err)
	}
}

func TestHandleCallback_LinkingFlowLinksAndSkipsLogin(t *testing.T) {
	db := setupTestDB(t)
	providerSvc := NewOIDCProviderService(db, testCookieKey)
	loginSvc := NewOIDCLoginService(db, providerSvc, testCookieKey)
	idp := newFakeIDP(t, "test-client")
	provider, _ := providerSvc.Create("test", "Test", idp.server.URL, "test-client", "test-secret", "openid,email,profile")

	user := createTestUser(t, NewUserService(db), "linker@example.com", "Lnk", "Er")

	state, cookie, nonce, code := idpClientHelper(t, loginSvc, provider.ID, idp.server.URL+"/cb", user.ID)
	idp.nextSub = fmt.Sprintf("ext-%d", user.ID)
	idp.nextEmail = "linker@example.com"
	idp.nextNonce = nonce

	outcome, err := loginSvc.HandleCallback(context.Background(), cookie, state, code)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if outcome.LinkedUserID != user.ID {
		t.Errorf("expected LinkedUserID=%d, got %d", user.ID, outcome.LinkedUserID)
	}
	if outcome.LoggedInUser != nil {
		t.Error("linking flow should not return LoggedInUser")
	}
	ids, _ := loginSvc.ListIdentitiesForUser(user.ID)
	if len(ids) != 1 {
		t.Errorf("expected 1 identity after link, got %d", len(ids))
	}
}
