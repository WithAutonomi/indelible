package services

import (
	"database/sql"
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
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// OIDCProviderService handles OIDC provider configuration.
type OIDCProviderService struct {
	db            *database.DB
	encryptionKey string
}

// NewOIDCProviderService creates a new OIDCProviderService.
func NewOIDCProviderService(db *database.DB, encryptionKey string) *OIDCProviderService {
	return &OIDCProviderService{db: db, encryptionKey: encryptionKey}
}

// Create adds a new OIDC provider. The client secret is encrypted at rest.
func (s *OIDCProviderService) Create(name, displayName, issuerURL, clientID, clientSecret, scopes string) (*OIDCProvider, error) {
	encryptedSecret, err := crypto.Encrypt(s.encryptionKey, clientSecret)
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
	err := s.db.QueryRow(
		`SELECT id, name, display_name, issuer_url, client_id, client_secret, scopes, is_enabled, created_at, updated_at FROM oidc_providers WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.IssuerURL, &p.ClientID, &p.EncryptedSecret, &p.Scopes, &p.IsEnabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOIDCProviderNotFound
		}
		return nil, err
	}
	return p, nil
}

// List returns all OIDC providers.
func (s *OIDCProviderService) List() ([]*OIDCProvider, error) {
	rows, err := s.db.Query(
		`SELECT id, name, display_name, issuer_url, client_id, client_secret, scopes, is_enabled, created_at, updated_at FROM oidc_providers ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*OIDCProvider
	for rows.Next() {
		p := &OIDCProvider{}
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.IssuerURL, &p.ClientID, &p.EncryptedSecret, &p.Scopes, &p.IsEnabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// Update modifies an OIDC provider. If clientSecret is empty, the existing secret is kept.
func (s *OIDCProviderService) Update(id int64, name, displayName, issuerURL, clientID, clientSecret, scopes string, isEnabled bool) (*OIDCProvider, error) {
	if clientSecret != "" {
		encrypted, err := crypto.Encrypt(s.encryptionKey, clientSecret)
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
