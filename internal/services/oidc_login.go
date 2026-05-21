package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/WithAutonomi/indelible/internal/crypto"
	"github.com/WithAutonomi/indelible/internal/database"
)

// --- Public surface ---------------------------------------------------------

// OIDCStateCookieName is the cookie that carries the encrypted state/nonce/
// PKCE-verifier blob between authorize and callback. HttpOnly + Secure + Lax.
const OIDCStateCookieName = "oidc_login_state"

// OIDCStateCookieTTL bounds how long an in-flight login can sit between the
// authorize redirect and the user landing back on callback. Matches the spec
// recommendation in V2-272.
const OIDCStateCookieTTL = 10 * time.Minute

var (
	ErrOIDCStateCookieMissing = errors.New("oidc state cookie missing")
	ErrOIDCStateExpired       = errors.New("oidc state expired")
	ErrOIDCStateMismatch      = errors.New("oidc state mismatch")
	ErrOIDCNonceMismatch      = errors.New("oidc nonce mismatch")
	// ErrOIDCEmailCollision is raised when an unlinked local user with the same
	// email already exists. We refuse auto-provision rather than auto-link by
	// email — see "email-confusion guard" in V2-272.
	ErrOIDCEmailCollision = errors.New("oidc email already registered without external_id correlation")
	// ErrOIDCNoAccount means the (provider, sub) pair has never been seen,
	// SCIM externalId doesn't match either, and auto_provision is off for this
	// provider. Caller should redirect with ?error=no_account.
	ErrOIDCNoAccount = errors.New("no linked account for OIDC subject")
	// ErrOIDCMissingEmail is raised when auto-provisioning would require a
	// usable email and the IdP didn't return one (or returned email_verified=false).
	ErrOIDCMissingEmail        = errors.New("oidc claim missing verified email")
	ErrOIDCProviderDisabled    = errors.New("oidc provider is disabled")
	ErrOIDCIdentityLinkedOther = errors.New("oidc identity already linked to a different user")
	ErrOIDCCannotUnlinkLast    = errors.New("cannot unlink the user's only remaining login method")
)

// CallbackOutcome describes what HandleCallback resolved an incoming IdP
// response to. Exactly one of LoggedInUser / LinkedUserID will be set on
// success; on error the caller routes to the appropriate /login error page.
type CallbackOutcome struct {
	LoggedInUser *User // populated on a normal login flow
	IsNewUser    bool  // true when auto-provisioning created the user
	LinkedUserID int64 // populated when the cookie carried link_to_user_id
}

// OIDCIdentity is a row in oidc_identities — one per (provider, subject) pair
// linked to a local user.
type OIDCIdentity struct {
	ID         int64
	UserID     int64
	ProviderID int64
	Subject    string
	CreatedAt  time.Time
}

// --- Service ----------------------------------------------------------------

// OIDCLoginService orchestrates authorize → IdP → callback round-trip,
// including state/nonce/PKCE handling and the identity-matching decision tree.
// It reuses the wallet encryption key for state-cookie AEAD so deployments
// don't need a separate secret.
type OIDCLoginService struct {
	db          *database.DB
	providerSvc *OIDCProviderService
	userSvc     *UserService
	groupSvc    *GroupService
	cookieKey   string // hex-encoded AES-256 key (same as wallet key)
	now         func() time.Time
}

func NewOIDCLoginService(db *database.DB, providerSvc *OIDCProviderService, cookieKey string) *OIDCLoginService {
	return &OIDCLoginService{
		db:          db,
		providerSvc: providerSvc,
		userSvc:     NewUserService(db),
		groupSvc:    NewGroupService(db),
		cookieKey:   cookieKey,
		now:         time.Now,
	}
}

// AuthorizeOpts captures the per-flow details the service needs from the
// caller. RedirectURL must point at the registered OIDC callback in this
// instance; LinkToUserID, when non-zero, switches the flow into "link to an
// existing logged-in user" mode (the callback creates an oidc_identities row
// instead of logging anyone in).
type AuthorizeOpts struct {
	RedirectURL  string
	LinkToUserID int64
}

// BuildAuthorizeURL discovers the IdP, generates state/nonce/PKCE-verifier,
// builds the authorize URL, and returns it alongside the opaque cookie value
// the caller must set on the response.
func (s *OIDCLoginService) BuildAuthorizeURL(ctx context.Context, providerID int64, opts AuthorizeOpts) (authURL, cookieValue string, err error) {
	provider, err := s.providerSvc.GetByID(providerID)
	if err != nil {
		return "", "", err
	}
	if !provider.IsEnabled {
		return "", "", ErrOIDCProviderDisabled
	}

	idp, err := oidc.NewProvider(ctx, provider.IssuerURL)
	if err != nil {
		return "", "", fmt.Errorf("oidc discovery: %w", err)
	}

	secret, err := crypto.Decrypt(s.cookieKey, provider.EncryptedSecret)
	if err != nil {
		return "", "", fmt.Errorf("decrypt provider secret: %w", err)
	}

	stateToken := randHex(32)
	nonce := randHex(32)
	verifier := oauth2.GenerateVerifier()

	cfg := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: secret,
		Endpoint:     idp.Endpoint(),
		RedirectURL:  opts.RedirectURL,
		Scopes:       splitScopes(provider.Scopes),
	}

	// Base options: nonce + PKCE challenge. Append per-provider extras
	// (Google Workspace hd=, MS prompt=, AAD domain_hint, …) afterwards so
	// they never clobber the SDK-managed params.
	authOpts := []oauth2.AuthCodeOption{
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	}
	for k, v := range provider.ExtraAuthorizeParams {
		authOpts = append(authOpts, oauth2.SetAuthURLParam(k, v))
	}
	authURL = cfg.AuthCodeURL(stateToken, authOpts...)

	payload := oidcStatePayload{
		ProviderID:   providerID,
		State:        stateToken,
		Nonce:        nonce,
		CodeVerifier: verifier,
		RedirectURL:  opts.RedirectURL,
		LinkToUserID: opts.LinkToUserID,
		Exp:          s.now().Add(OIDCStateCookieTTL).Unix(),
	}
	cookieValue, err = encodeOIDCStateCookie(s.cookieKey, payload)
	if err != nil {
		return "", "", err
	}
	return authURL, cookieValue, nil
}

// HandleCallback completes the authorization-code exchange and resolves the
// returned identity against local state. Always clear the OIDCStateCookieName
// cookie on the response after this returns, success or failure.
func (s *OIDCLoginService) HandleCallback(ctx context.Context, cookieValue, queryState, code string) (*CallbackOutcome, error) {
	if cookieValue == "" {
		return nil, ErrOIDCStateCookieMissing
	}
	payload, err := decodeOIDCStateCookie(s.cookieKey, cookieValue)
	if err != nil {
		return nil, fmt.Errorf("decode state cookie: %w", err)
	}
	if s.now().Unix() > payload.Exp {
		return nil, ErrOIDCStateExpired
	}
	if queryState == "" || queryState != payload.State {
		return nil, ErrOIDCStateMismatch
	}

	provider, err := s.providerSvc.GetByID(payload.ProviderID)
	if err != nil {
		return nil, err
	}
	if !provider.IsEnabled {
		return nil, ErrOIDCProviderDisabled
	}

	idp, err := oidc.NewProvider(ctx, provider.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	secret, err := crypto.Decrypt(s.cookieKey, provider.EncryptedSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt provider secret: %w", err)
	}

	cfg := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: secret,
		Endpoint:     idp.Endpoint(),
		RedirectURL:  payload.RedirectURL,
		Scopes:       splitScopes(provider.Scopes),
	}

	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(payload.CodeVerifier))
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("oidc: no id_token in token response")
	}

	verifier := idp.Verifier(&oidc.Config{ClientID: provider.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	if idToken.Nonce != payload.Nonce {
		return nil, ErrOIDCNonceMismatch
	}

	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		GivenName     string `json:"given_name"`
		FamilyName    string `json:"family_name"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse id_token claims: %w", err)
	}
	if claims.Sub == "" {
		return nil, errors.New("oidc: id_token missing sub claim")
	}

	// Linking flow: the cookie pinned this round-trip to a specific
	// already-logged-in user. We don't care about email collisions because
	// the user explicitly asked to add this provider.
	if payload.LinkToUserID > 0 {
		if err := s.LinkIdentity(payload.LinkToUserID, payload.ProviderID, claims.Sub); err != nil {
			return nil, err
		}
		return &CallbackOutcome{LinkedUserID: payload.LinkToUserID}, nil
	}

	// Login flow: walk the identity-matching tree.
	user, isNew, err := s.resolveOrProvisionUser(payload.ProviderID, claims.Sub, claims.Email, claims.EmailVerified, claims.GivenName, claims.FamilyName, claims.Name, provider)
	if err != nil {
		return nil, err
	}
	return &CallbackOutcome{LoggedInUser: user, IsNewUser: isNew}, nil
}

// resolveOrProvisionUser implements V2-272's identity matching:
//  1. (provider_id, sub) hit → log in as that user
//  2. users.external_id matches sub → auto-link (SCIM correlation) and log in
//  3. auto_provision=false → no_account
//  4. auto_provision=true + email already used → email-collision (never link by email)
//  5. auto_provision=true + email free → create user + default group, log in
func (s *OIDCLoginService) resolveOrProvisionUser(providerID int64, sub, email string, emailVerified bool, givenName, familyName, name string, provider *OIDCProvider) (*User, bool, error) {
	// 1) Direct (provider_id, sub) match.
	if uid, err := s.lookupIdentityUserID(providerID, sub); err == nil {
		u, err := s.userSvc.GetByID(uid)
		return u, false, err
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	// 2) SCIM correlation: users.external_id == sub.
	if u, err := s.userSvc.GetByExternalID(sub); err == nil {
		if err := s.LinkIdentity(u.ID, providerID, sub); err != nil {
			return nil, false, err
		}
		return u, false, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, false, err
	}

	// Not linked; decide whether to auto-provision.
	if !provider.AutoProvision {
		return nil, false, ErrOIDCNoAccount
	}
	if email == "" || !emailVerified {
		return nil, false, ErrOIDCMissingEmail
	}

	// 4) Email-collision guard — never auto-link by email.
	if existing, err := s.userSvc.GetByEmail(email); err == nil && existing != nil {
		return nil, false, ErrOIDCEmailCollision
	} else if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, false, err
	}

	// 5) Create user (SCIM-style: no password) + identity + optional group.
	first, last := splitName(givenName, familyName, name)
	created, err := s.userSvc.CreateFromSCIM(email, first, last, sub)
	if err != nil {
		return nil, false, err
	}
	if err := s.LinkIdentity(created.ID, providerID, sub); err != nil {
		return nil, false, err
	}
	if provider.DefaultGroupID.Valid {
		// Pass 0 → NULL added_by (no acting user); ignore "already member"
		// (impossible on a fresh user) but bubble real errors.
		if err := s.groupSvc.AddMember(provider.DefaultGroupID.Int64, created.ID, 0); err != nil && !errors.Is(err, ErrAlreadyMember) {
			return nil, false, fmt.Errorf("add to default group: %w", err)
		}
	}
	return created, true, nil
}

// LinkIdentity inserts an oidc_identities row tying the given user to (provider, sub).
// Idempotent for the same user; returns ErrOIDCIdentityLinkedOther when the
// (provider, sub) is already claimed by a different user.
func (s *OIDCLoginService) LinkIdentity(userID, providerID int64, sub string) error {
	if existing, err := s.lookupIdentityUserID(providerID, sub); err == nil {
		if existing == userID {
			return nil
		}
		return ErrOIDCIdentityLinkedOther
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO oidc_identities (user_id, provider_id, subject) VALUES (?, ?, ?)`,
		userID, providerID, sub,
	)
	return err
}

// UnlinkIdentity removes an oidc_identities row, refusing if it would leave
// the user with no way to log in (no password AND this is their only identity).
func (s *OIDCLoginService) UnlinkIdentity(identityID, currentUserID int64) error {
	var ownerID int64
	err := s.db.QueryRow(
		`SELECT user_id FROM oidc_identities WHERE id = ?`, identityID,
	).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("oidc identity not found")
	}
	if err != nil {
		return err
	}
	if ownerID != currentUserID {
		return errors.New("oidc identity does not belong to current user")
	}

	user, err := s.userSvc.GetByID(currentUserID)
	if err != nil {
		return err
	}
	hasPassword := user.PasswordHash.Valid && user.PasswordHash.String != ""
	count, err := s.countIdentities(currentUserID)
	if err != nil {
		return err
	}
	// Last login method check: no password and this is their only identity.
	if !hasPassword && count <= 1 {
		return ErrOIDCCannotUnlinkLast
	}

	_, err = s.db.Exec(`DELETE FROM oidc_identities WHERE id = ?`, identityID)
	return err
}

// ListIdentitiesForUser returns the user's linked identities, for the profile
// "connected accounts" view.
func (s *OIDCLoginService) ListIdentitiesForUser(userID int64) ([]*OIDCIdentity, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, provider_id, subject, created_at FROM oidc_identities WHERE user_id = ? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*OIDCIdentity
	for rows.Next() {
		id := &OIDCIdentity{}
		if err := rows.Scan(&id.ID, &id.UserID, &id.ProviderID, &id.Subject, &id.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *OIDCLoginService) lookupIdentityUserID(providerID int64, sub string) (int64, error) {
	var uid int64
	err := s.db.QueryRow(
		`SELECT user_id FROM oidc_identities WHERE provider_id = ? AND subject = ?`,
		providerID, sub,
	).Scan(&uid)
	return uid, err
}

func (s *OIDCLoginService) countIdentities(userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM oidc_identities WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// --- Cookie helpers ---------------------------------------------------------

// oidcStatePayload is the JSON shape encrypted inside the state cookie.
type oidcStatePayload struct {
	ProviderID   int64  `json:"p"`
	State        string `json:"s"`
	Nonce        string `json:"n"`
	CodeVerifier string `json:"v"`
	RedirectURL  string `json:"r"`
	LinkToUserID int64  `json:"l,omitempty"`
	Exp          int64  `json:"e"`
}

func encodeOIDCStateCookie(key string, p oidcStatePayload) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	enc, err := crypto.Encrypt(key, string(raw))
	if err != nil {
		return "", err
	}
	// crypto.Encrypt returns hex; wrap in base64 so the cookie value is a
	// single URL-safe token. Slightly bigger but easier to debug than raw hex.
	return base64.RawURLEncoding.EncodeToString([]byte(enc)), nil
}

func decodeOIDCStateCookie(key, cookieValue string) (oidcStatePayload, error) {
	var p oidcStatePayload
	encBytes, err := base64.RawURLEncoding.DecodeString(cookieValue)
	if err != nil {
		return p, fmt.Errorf("base64 decode: %w", err)
	}
	plain, err := crypto.Decrypt(key, string(encBytes))
	if err != nil {
		return p, fmt.Errorf("aes decrypt: %w", err)
	}
	if err := json.Unmarshal([]byte(plain), &p); err != nil {
		return p, fmt.Errorf("json decode: %w", err)
	}
	return p, nil
}

// --- Misc helpers -----------------------------------------------------------

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// Should never happen on a healthy system.
		panic("oidc: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func splitScopes(s string) []string {
	if s == "" {
		return []string{oidc.ScopeOpenID, "email", "profile"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seenOpenID := false
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		if t == oidc.ScopeOpenID {
			seenOpenID = true
		}
		out = append(out, t)
	}
	if !seenOpenID {
		out = append([]string{oidc.ScopeOpenID}, out...)
	}
	return out
}

// splitName resolves a (givenName, familyName, name) triple into a usable
// (first, last) pair. IdPs vary wildly — Google sends both givenName and
// familyName, some only send `name`. Empty results are fine; the user model
// tolerates blank first/last.
func splitName(given, family, full string) (string, string) {
	given = strings.TrimSpace(given)
	family = strings.TrimSpace(family)
	if given != "" || family != "" {
		return given, family
	}
	full = strings.TrimSpace(full)
	if full == "" {
		return "", ""
	}
	parts := strings.SplitN(full, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
