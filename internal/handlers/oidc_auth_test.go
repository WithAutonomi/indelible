package handlers_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// --- Fake IdP --------------------------------------------------------------
//
// Minimal stand-in for an external OIDC IdP. Serves discovery, JWKS, token,
// and a stubbed authorize endpoint that auto-redirects back to the indelible
// callback with a known code (simulating instant user consent).

type fakeIDP struct {
	t        *testing.T
	server   *httptest.Server
	key      *rsa.PrivateKey
	jwks     string
	clientID string

	// What the next signed ID token claims should look like.
	sub, email, name string
	emailVerified    bool

	// Captured from the most recent /authorize call so the matching
	// /token response signs the same nonce into the id_token.
	lastNonce string
}

func startFakeIDP(t *testing.T, clientID string) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	idp := &fakeIDP{t: t, key: key, clientID: clientID, emailVerified: true}

	jwk := jose.JSONWebKey{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig", KeyID: "k1"}
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
		_ = r.ParseForm()
		idToken := idp.signIDToken(idp.sub, idp.email, idp.emailVerified, idp.lastNonce, idp.name)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access",
			"token_type":   "Bearer",
			"id_token":     idToken,
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		idp.lastNonce = r.URL.Query().Get("nonce")
		state := r.URL.Query().Get("state")
		redirectURI := r.URL.Query().Get("redirect_uri")
		http.Redirect(w, r, redirectURI+"?code=fake-code&state="+state, http.StatusFound)
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIDP) signIDToken(sub, email string, emailVerified bool, nonce, name string) string {
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"),
	)
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
		"name":           name,
	}
	raw, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		f.t.Fatalf("sign: %v", err)
	}
	return raw
}

// --- Test fixture ---------------------------------------------------------

type oidcEnv struct {
	router     http.Handler
	cfg        *config.Config
	db         *database.DB
	idp        *fakeIDP
	providerID int64
}

func setupOIDCTest(t *testing.T) *oidcEnv {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             "http://localhost:8082",
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	db := dbtest.OpenDB(t)
	router := handlers.NewRouter(cfg, db, nil)

	idp := startFakeIDP(t, "test-client")
	providerSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	p, err := providerSvc.Create("test", "Test IdP", idp.server.URL, "test-client", "test-secret", "openid,email,profile")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return &oidcEnv{router: router, cfg: cfg, db: db, idp: idp, providerID: p.ID}
}

// noFollowClient drives requests against a real httptest.Server so cookies
// survive redirects, but does NOT follow them — the tests need to inspect
// each hop's Set-Cookie + Location.
type noFollowClient struct {
	server *httptest.Server
	jar    *cookieJar
}

func newNoFollowClient(t *testing.T, router http.Handler) *noFollowClient {
	t.Helper()
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &noFollowClient{server: srv, jar: newCookieJar()}
}

func (c *noFollowClient) do(method, path string) *http.Response {
	req, err := http.NewRequest(method, c.server.URL+path, nil)
	if err != nil {
		panic(err)
	}
	c.jar.applyTo(req)
	cli := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cli.Do(req)
	if err != nil {
		panic(err)
	}
	c.jar.captureFrom(resp)
	return resp
}

// followExternalRedirect issues a GET to the Location of the given response,
// carrying cookies. Used to walk through the authorize → IdP → callback chain.
func (c *noFollowClient) followExternalRedirect(resp *http.Response) *http.Response {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return resp
	}
	req, err := http.NewRequest("GET", loc, nil)
	if err != nil {
		panic(err)
	}
	c.jar.applyTo(req)
	cli := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp2, err := cli.Do(req)
	if err != nil {
		panic(err)
	}
	c.jar.captureFrom(resp2)
	return resp2
}

// Minimal cookie jar — std http.CookieJar wants public-suffix logic we don't
// need here.
type cookieJar struct {
	cookies []*http.Cookie
}

func newCookieJar() *cookieJar { return &cookieJar{} }

func (j *cookieJar) captureFrom(resp *http.Response) {
	for _, c := range resp.Cookies() {
		j.cookies = append([]*http.Cookie{c}, j.cookies...)
	}
}

func (j *cookieJar) applyTo(req *http.Request) {
	seen := map[string]bool{}
	for _, c := range j.cookies {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		if c.MaxAge < 0 {
			continue
		}
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
}

// --- Tests ----------------------------------------------------------------

func TestListOIDCProviders_OnlyEnabledShown(t *testing.T) {
	env := setupOIDCTest(t)
	providerSvc := services.NewOIDCProviderService(env.db, env.cfg.WalletEncryptionKey)
	disabled, _ := providerSvc.Create("disabled", "Disabled", "https://example.com", "cid", "sec", "openid")
	_, _ = providerSvc.Update(disabled.ID, "disabled", "Disabled", "https://example.com", "cid", "", "openid,email,profile", false)

	req := httptest.NewRequest("GET", "/api/v2/auth/oidc/providers", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	providers, _ := resp["providers"].([]any)
	if len(providers) != 1 {
		t.Errorf("expected 1 enabled provider, got %d", len(providers))
	}
	if p, ok := providers[0].(map[string]any); ok {
		if _, hasClient := p["client_id"]; hasClient {
			t.Error("public provider response leaked client_id")
		}
	}
}

func TestOIDCAuthorize_SetsCookieAndRedirectsToIdP(t *testing.T) {
	env := setupOIDCTest(t)
	cli := newNoFollowClient(t, env.router)

	resp := cli.do("GET", fmt.Sprintf("/api/v2/auth/oidc/authorize/%d", env.providerID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 302, got %d body=%s", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, env.idp.server.URL+"/authorize") {
		t.Errorf("expected redirect to IdP authorize, got %q", loc)
	}
	if !strings.Contains(loc, "code_challenge=") || !strings.Contains(loc, "code_challenge_method=S256") {
		t.Errorf("authorize URL missing PKCE: %s", loc)
	}

	gotStateCookie := false
	for _, c := range resp.Cookies() {
		if c.Name == services.OIDCStateCookieName {
			gotStateCookie = true
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("state cookie should be HttpOnly + SameSite=Lax, got HttpOnly=%v SameSite=%v",
					c.HttpOnly, c.SameSite)
			}
		}
	}
	if !gotStateCookie {
		t.Error("state cookie was not set")
	}
}

func TestOIDCAuthorize_404ForUnknownProvider(t *testing.T) {
	env := setupOIDCTest(t)
	cli := newNoFollowClient(t, env.router)
	resp := cli.do("GET", "/api/v2/auth/oidc/authorize/9999")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestOIDCCallback_EndToEnd_LoginExistingIdentity(t *testing.T) {
	env := setupOIDCTest(t)

	// Pre-create the user + identity so the callback resolves via (provider, sub).
	userSvc := services.NewUserService(env.db)
	user, err := userSvc.Create("alice@example.com", "$2a$10$ignored", "Alice", "S")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	providerSvc := services.NewOIDCProviderService(env.db, env.cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(env.db, providerSvc, env.cfg.WalletEncryptionKey)
	if err := loginSvc.LinkIdentity(user.ID, env.providerID, "alice-sub"); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	env.idp.sub = "alice-sub"
	env.idp.email = "alice@example.com"
	env.idp.name = "Alice S"

	cli := newNoFollowClient(t, env.router)

	// 1) Authorize → 302 to fake IdP.
	resp := cli.do("GET", fmt.Sprintf("/api/v2/auth/oidc/authorize/%d", env.providerID))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: got %d", resp.StatusCode)
	}
	// 2) Fake IdP /authorize → 302 back to our callback with code+state.
	cbResp := cli.followExternalRedirect(resp)
	if cbResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("IdP authorize: got %d body=%s", cbResp.StatusCode, body)
	}
	loc := cbResp.Header.Get("Location")
	if !strings.HasPrefix(loc, cli.server.URL+"/api/v2/auth/oidc/callback") {
		t.Fatalf("IdP didn't redirect back to callback, got %q", loc)
	}
	// 3) Our /callback → resolves identity, sets session cookie, 302 to /.
	cbCallback := cli.followExternalRedirect(cbResp)
	defer cbCallback.Body.Close()
	if cbCallback.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(cbCallback.Body)
		t.Fatalf("callback: got %d body=%s loc=%s", cbCallback.StatusCode, body, cbCallback.Header.Get("Location"))
	}
	loc = cbCallback.Header.Get("Location")
	if !strings.HasPrefix(loc, "/") || strings.Contains(loc, "error=") {
		t.Errorf("expected redirect to /, got %q", loc)
	}
	gotSession := false
	for _, c := range cbCallback.Cookies() {
		if c.Name == "session" && c.Value != "" {
			gotSession = true
		}
	}
	if !gotSession {
		t.Error("expected session cookie after successful callback")
	}
}

func TestOIDCCallback_NoAccountRedirectsWithError(t *testing.T) {
	env := setupOIDCTest(t)
	env.idp.sub = "unknown"
	env.idp.email = "nobody@example.com"

	cli := newNoFollowClient(t, env.router)
	resp := cli.do("GET", fmt.Sprintf("/api/v2/auth/oidc/authorize/%d", env.providerID))
	cbResp := cli.followExternalRedirect(resp)
	cb := cli.followExternalRedirect(cbResp)
	defer cb.Body.Close()

	loc := cb.Header.Get("Location")
	if !strings.HasPrefix(loc, "/login?error=no_account") {
		t.Errorf("expected /login?error=no_account, got %q", loc)
	}
}

func TestOIDCCallback_IdPErrorPassesThrough(t *testing.T) {
	env := setupOIDCTest(t)
	cli := newNoFollowClient(t, env.router)
	resp := cli.do("GET", "/api/v2/auth/oidc/callback?error=access_denied")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/login?error=access_denied") {
		t.Errorf("expected /login?error=access_denied, got %q", loc)
	}
}

func TestOIDCUnlinkIdentity_RequiresAuth(t *testing.T) {
	env := setupOIDCTest(t)
	req := httptest.NewRequest("DELETE", "/api/v2/auth/oidc/identities/1", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}
}

func TestOIDCUnlinkIdentity_LastLoginMethodReturns409(t *testing.T) {
	env := setupOIDCTest(t)

	// SCIM-style user (no password) + single linked identity → unlink must 409.
	userSvc := services.NewUserService(env.db)
	scimUser, err := userSvc.CreateFromSCIM("scim@example.com", "S", "U", "ext-1")
	if err != nil {
		t.Fatalf("CreateFromSCIM: %v", err)
	}
	providerSvc := services.NewOIDCProviderService(env.db, env.cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(env.db, providerSvc, env.cfg.WalletEncryptionKey)
	if err := loginSvc.LinkIdentity(scimUser.ID, env.providerID, "ext-1"); err != nil {
		t.Fatalf("link: %v", err)
	}
	ids, _ := loginSvc.ListIdentitiesForUser(scimUser.ID)

	// Hand-mint a token for the SCIM user (no password → can't use Login API).
	token, err := auth.GenerateToken(env.cfg.JWTSecret, scimUser.ID, scimUser.Email, 1)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	req := httptest.NewRequest("DELETE",
		fmt.Sprintf("/api/v2/auth/oidc/identities/%d", ids[0].ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 (last login method), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestListMyOIDCIdentities_ReturnsLinked(t *testing.T) {
	env := setupOIDCTest(t)
	adminToken := registerAndGetToken(t, env.router, "admin@test.com", "password123", "Admin", "U")
	userSvc := services.NewUserService(env.db)
	admin, err := userSvc.GetByEmail("admin@test.com")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	providerSvc := services.NewOIDCProviderService(env.db, env.cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(env.db, providerSvc, env.cfg.WalletEncryptionKey)
	if err := loginSvc.LinkIdentity(admin.ID, env.providerID, "admin-sub"); err != nil {
		t.Fatalf("link: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v2/me/oidc/identities", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	ids, _ := resp["identities"].([]any)
	if len(ids) != 1 {
		t.Errorf("expected 1 identity, got %d", len(ids))
	}
	if m, ok := ids[0].(map[string]any); ok {
		if m["subject"] != "admin-sub" {
			t.Errorf("subject = %v", m["subject"])
		}
		if m["provider_name"] != "Test IdP" {
			t.Errorf("provider_name = %v", m["provider_name"])
		}
	}
}

func TestAdminSetOIDCAutoProvision_PersistsChanges(t *testing.T) {
	env := setupOIDCTest(t)
	adminToken := registerAndGetToken(t, env.router, "admin@test.com", "password123", "Admin", "U")

	body, _ := json.Marshal(map[string]any{"auto_provision": true, "default_group_id": 0})
	req := httptest.NewRequest("PUT",
		fmt.Sprintf("/api/v2/admin/oidc/providers/%d/auto-provision", env.providerID),
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", w.Code, w.Body.String())
	}
	providerSvc := services.NewOIDCProviderService(env.db, env.cfg.WalletEncryptionKey)
	p, _ := providerSvc.GetByID(env.providerID)
	if !p.AutoProvision {
		t.Error("auto_provision was not persisted")
	}
}

func TestAdminSetOIDCRequireEmailVerified_PersistsChanges(t *testing.T) {
	env := setupOIDCTest(t)
	adminToken := registerAndGetToken(t, env.router, "admin@test.com", "password123", "Admin", "U")

	providerSvc := services.NewOIDCProviderService(env.db, env.cfg.WalletEncryptionKey)
	if before, _ := providerSvc.GetByID(env.providerID); !before.RequireEmailVerified {
		t.Fatalf("expected default require_email_verified=true, got false")
	}

	body, _ := json.Marshal(map[string]any{"require_email_verified": false})
	req := httptest.NewRequest("PUT",
		fmt.Sprintf("/api/v2/admin/oidc/providers/%d/require-email-verified", env.providerID),
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", w.Code, w.Body.String())
	}
	p, _ := providerSvc.GetByID(env.providerID)
	if p.RequireEmailVerified {
		t.Error("require_email_verified was not persisted to false")
	}
}
