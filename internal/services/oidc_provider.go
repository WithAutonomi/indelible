package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/WithAutonomi/indelible/internal/crypto"
	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrOIDCProviderNotFound = errors.New("OIDC provider not found")
)

// OIDCProvider represents an OIDC/SSO provider configuration.
type OIDCProvider struct {
	ID              int64
	Name            string
	DisplayName     string
	IssuerURL       string
	ClientID        string
	EncryptedSecret string
	Scopes          string
	IsEnabled       bool
	// DefaultGroupID is the group new users join when auto-provisioned via this
	// provider. NULL = no automatic group assignment.
	DefaultGroupID sql.NullInt64
	// AutoProvision controls whether an unknown (sub, email) pair creates a new
	// local user on first login (true) or is rejected with no_account (false).
	AutoProvision bool
	// RequireEmailVerified gates the email_verified claim check during
	// auto-provisioning. Strict by default. Operators turn this off for IdPs
	// that don't emit the claim — Okta integrator tenants, Azure AD, and
	// generic OIDC providers per §5.1 of OIDC Core (where the claim is
	// optional). The email-collision guard still applies regardless.
	RequireEmailVerified bool
	// ExtraAuthorizeParams are appended as query params to the IdP authorize
	// URL. Primary use case is Google Workspace domain restriction via
	// hd=company.com — without this an "External" Google OAuth client would
	// accept any personal @gmail.com signin. Also fits Microsoft prompt=
	// and AAD domain_hint. Empty map = no extra params (the common case).
	ExtraAuthorizeParams map[string]string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// OIDCProviderService handles OIDC provider configuration.
type OIDCProviderService struct {
	db *database.DB
	kr *crypto.Keyring // wallet/OIDC encryption keyring (client secrets share the wallet key)
}

// NewOIDCProviderService creates a new OIDCProviderService. kr is the
// wallet-encryption keyring from the secrets provider (cfg.WalletKeyring()).
// kr may be nil for read-only callers (List/GetByID never touch it); only
// Create and Update encrypt the client secret.
func NewOIDCProviderService(db *database.DB, kr *crypto.Keyring) *OIDCProviderService {
	return &OIDCProviderService{db: db, kr: kr}
}

// Create adds a new OIDC provider. The client secret is encrypted at rest.
func (s *OIDCProviderService) Create(name, displayName, issuerURL, clientID, clientSecret, scopes string) (*OIDCProvider, error) {
	// Key-id-tagged envelope so the secret survives a wallet/OIDC key rotation (V2-448).
	encryptedSecret, err := s.kr.Encrypt(clientSecret)
	if err != nil {
		return nil, err
	}

	if scopes == "" {
		scopes = "openid,email,profile"
	}

	var id int64
	err = s.db.QueryRow(
		`INSERT INTO oidc_providers (name, display_name, issuer_url, client_id, client_secret, scopes) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		name, displayName, issuerURL, clientID, encryptedSecret, scopes,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

// GetByID retrieves an OIDC provider by ID.
func (s *OIDCProviderService) GetByID(id int64) (*OIDCProvider, error) {
	p := &OIDCProvider{}
	var rawExtra string
	err := s.db.QueryRow(
		`SELECT id, name, display_name, issuer_url, client_id, client_secret, scopes, is_enabled, default_group_id, auto_provision, require_email_verified, extra_authorize_params, created_at, updated_at FROM oidc_providers WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.IssuerURL, &p.ClientID, &p.EncryptedSecret, &p.Scopes, &p.IsEnabled, &p.DefaultGroupID, &p.AutoProvision, &p.RequireEmailVerified, &rawExtra, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOIDCProviderNotFound
		}
		return nil, err
	}
	p.ExtraAuthorizeParams = unmarshalExtraAuthorizeParams(rawExtra)
	return p, nil
}

// List returns all OIDC providers.
func (s *OIDCProviderService) List() ([]*OIDCProvider, error) {
	rows, err := s.db.Query(
		`SELECT id, name, display_name, issuer_url, client_id, client_secret, scopes, is_enabled, default_group_id, auto_provision, require_email_verified, extra_authorize_params, created_at, updated_at FROM oidc_providers ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*OIDCProvider
	for rows.Next() {
		p := &OIDCProvider{}
		var rawExtra string
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.IssuerURL, &p.ClientID, &p.EncryptedSecret, &p.Scopes, &p.IsEnabled, &p.DefaultGroupID, &p.AutoProvision, &p.RequireEmailVerified, &rawExtra, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.ExtraAuthorizeParams = unmarshalExtraAuthorizeParams(rawExtra)
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// Update modifies an OIDC provider. If clientSecret is empty, the existing secret is kept.
func (s *OIDCProviderService) Update(id int64, name, displayName, issuerURL, clientID, clientSecret, scopes string, isEnabled bool) (*OIDCProvider, error) {
	if clientSecret != "" {
		encrypted, err := s.kr.Encrypt(clientSecret)
		if err != nil {
			return nil, err
		}
		_, err = s.db.Exec(
			`UPDATE oidc_providers SET name = ?, display_name = ?, issuer_url = ?, client_id = ?, client_secret = ?, scopes = ?, is_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			name, displayName, issuerURL, clientID, encrypted, scopes, isEnabled, id,
		)
		if err != nil {
			return nil, err
		}
	} else {
		_, err := s.db.Exec(
			`UPDATE oidc_providers SET name = ?, display_name = ?, issuer_url = ?, client_id = ?, scopes = ?, is_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			name, displayName, issuerURL, clientID, scopes, isEnabled, id,
		)
		if err != nil {
			return nil, err
		}
	}
	return s.GetByID(id)
}

// SetExtraAuthorizeParams replaces the per-provider authorize-URL params map
// (e.g. {"hd": "company.com"} for Google Workspace). Pass an empty/nil map to
// clear. Mirrors SetAutoProvision's partial-update pattern so admin UI can
// edit this without re-supplying every other field.
func (s *OIDCProviderService) SetExtraAuthorizeParams(id int64, params map[string]string) error {
	raw := marshalExtraAuthorizeParams(params)
	_, err := s.db.Exec(
		`UPDATE oidc_providers SET extra_authorize_params = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		raw, id,
	)
	return err
}

// SetAutoProvision updates the auto_provision + default_group_id columns
// independently of the main Update method so admin UI can toggle them without
// re-supplying every other field. Pass defaultGroupID = 0 to clear it.
func (s *OIDCProviderService) SetAutoProvision(id int64, autoProvision bool, defaultGroupID int64) error {
	var gid sql.NullInt64
	if defaultGroupID > 0 {
		gid = sql.NullInt64{Int64: defaultGroupID, Valid: true}
	}
	_, err := s.db.Exec(
		`UPDATE oidc_providers SET auto_provision = ?, default_group_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		autoProvision, gid, id,
	)
	return err
}

// SetRequireEmailVerified toggles whether auto-provisioning enforces the
// id_token's email_verified claim for this provider. Mirrors SetAutoProvision's
// partial-update pattern.
func (s *OIDCProviderService) SetRequireEmailVerified(id int64, require bool) error {
	_, err := s.db.Exec(
		`UPDATE oidc_providers SET require_email_verified = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		require, id,
	)
	return err
}

// marshalExtraAuthorizeParams encodes the map for storage. Empty/nil → ""
// (the column default), so we never write "{}" or "null" to the DB.
func marshalExtraAuthorizeParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	raw, err := json.Marshal(params)
	if err != nil {
		// json.Marshal cannot fail on map[string]string; defensive only.
		return ""
	}
	return string(raw)
}

// unmarshalExtraAuthorizeParams decodes the DB column. Empty string or any
// parse error returns nil — the auth-URL builder treats nil as "no extra
// params", so a corrupt row degrades to vanilla OIDC rather than crashing.
func unmarshalExtraAuthorizeParams(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// Delete removes an OIDC provider and its linked identities.
func (s *OIDCProviderService) Delete(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM oidc_identities WHERE provider_id = ?`, id); err != nil {
		return err
	}

	result, err := tx.Exec(`DELETE FROM oidc_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrOIDCProviderNotFound
	}

	return tx.Commit()
}
