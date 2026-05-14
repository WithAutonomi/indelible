package services

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Setting represents a runtime configuration setting.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
	UpdatedBy sql.NullInt64
}

// ConfigAuditEntry represents a change to a setting.
type ConfigAuditEntry struct {
	ID         int64
	SettingKey string
	OldValue   sql.NullString
	NewValue   string
	ChangedBy  sql.NullInt64
	IPAddress  sql.NullString
	UserAgent  sql.NullString
	CreatedAt  time.Time
}

// SettingsService handles runtime settings.
type SettingsService struct {
	db *sql.DB
}

// NewSettingsService creates a new SettingsService.
func NewSettingsService(db *sql.DB) *SettingsService {
	return &SettingsService{db: db}
}

// GetAll returns all settings as a key-value map.
func (s *SettingsService) GetAll() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

// Get returns a single setting value.
func (s *SettingsService) Get(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, err
}

// Update sets one or more settings, logging each change to config_audit.
//
// Keys registered in typedValidators are validated up front; the entire PATCH
// is rejected with *ValidationError if any value fails. Untyped keys pass
// through unchanged for backward compatibility.
func (s *SettingsService) Update(changes map[string]string, userID int64, ipAddress, userAgent string) error {
	for key, newValue := range changes {
		if validator, ok := typedValidators[key]; ok {
			if err := validator(newValue); err != nil {
				return &ValidationError{Key: key, Reason: err.Error()}
			}
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for key, newValue := range changes {
		// Get old value
		var oldValue sql.NullString
		tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&oldValue)

		// Upsert setting
		_, err := tx.Exec(
			`INSERT INTO settings (key, value, updated_at, updated_by) VALUES (?, ?, CURRENT_TIMESTAMP, ?)
			 ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP, updated_by = ?`,
			key, newValue, userID, newValue, userID,
		)
		if err != nil {
			return err
		}

		// Log to config_audit
		_, err = tx.Exec(
			`INSERT INTO config_audit (setting_key, old_value, new_value, changed_by, ip_address, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
			key, oldValue, newValue, userID, ipAddress, userAgent,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// --- Export/Import ---

// ExportData is the structured export format including all instance configuration.
type ExportData struct {
	Settings      map[string]string  `json:"settings"`
	Webhooks      []ExportWebhook    `json:"webhooks,omitempty"`
	OIDCProviders []ExportOIDC       `json:"oidc_providers,omitempty"`
	Groups        []ExportGroup      `json:"groups,omitempty"`
}

// ExportWebhook is the webhook config in export format.
type ExportWebhook struct {
	URL             string `json:"url"`
	IntegrationType string `json:"integration_type"`
	IsEnabled       bool   `json:"is_enabled"`
	Events          string `json:"events"`
}

// ExportOIDC is the OIDC provider config in export format (no secrets).
type ExportOIDC struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	IssuerURL   string `json:"issuer_url"`
	ClientID    string `json:"client_id"`
	Scopes      string `json:"scopes"`
	IsEnabled   bool   `json:"is_enabled"`
}

// ExportGroup is the group config in export format.
type ExportGroup struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permission_level"`
	IsActive        bool   `json:"is_active"`
}

// Export returns all instance configuration as structured JSON.
func (s *SettingsService) Export() ([]byte, error) {
	data := ExportData{}

	// Settings
	settings, err := s.GetAll()
	if err != nil {
		return nil, err
	}
	data.Settings = settings

	// Webhooks
	whSvc := NewWebhookService(s.db)
	webhooks, err := whSvc.List()
	if err == nil {
		for _, w := range webhooks {
			data.Webhooks = append(data.Webhooks, ExportWebhook{
				URL:             w.URL,
				IntegrationType: w.IntegrationType,
				IsEnabled:       w.IsEnabled,
				Events:          w.Events,
			})
		}
	}

	// OIDC providers (without secrets)
	oidcSvc := NewOIDCProviderService(s.db, "")
	providers, err := oidcSvc.List()
	if err == nil {
		for _, p := range providers {
			data.OIDCProviders = append(data.OIDCProviders, ExportOIDC{
				Name:        p.Name,
				DisplayName: p.DisplayName,
				IssuerURL:   p.IssuerURL,
				ClientID:    p.ClientID,
				Scopes:      p.Scopes,
				IsEnabled:   p.IsEnabled,
			})
		}
	}

	// Groups
	grpSvc := NewGroupService(s.db)
	groups, err := grpSvc.List()
	if err == nil {
		for _, g := range groups {
			data.Groups = append(data.Groups, ExportGroup{
				Name:            g.Name,
				Description:     g.Description,
				PermissionLevel: g.PermissionLevel,
				IsActive:        g.IsActive,
			})
		}
	}

	return json.MarshalIndent(data, "", "  ")
}

// Import restores instance configuration from an export.
// Supports both the new structured format and the legacy flat settings format.
func (s *SettingsService) Import(data map[string]string, userID int64, ipAddress, userAgent string) error {
	return s.Update(data, userID, ipAddress, userAgent)
}

// ImportStructured restores full instance configuration from the structured export format.
func (s *SettingsService) ImportStructured(data *ExportData, userID int64, ipAddress, userAgent string) error {
	// Import settings
	if len(data.Settings) > 0 {
		if err := s.Update(data.Settings, userID, ipAddress, userAgent); err != nil {
			return err
		}
	}

	// Import webhooks
	if len(data.Webhooks) > 0 {
		whSvc := NewWebhookService(s.db)
		for _, w := range data.Webhooks {
			wh, err := whSvc.Create(w.URL, w.IntegrationType, w.Events)
			if err != nil {
				return err
			}
			if !w.IsEnabled {
				if _, err := whSvc.Update(wh.ID, wh.URL, wh.IntegrationType, wh.Events, false); err != nil {
					return err
				}
			}
		}
	}

	// Import groups
	if len(data.Groups) > 0 {
		grpSvc := NewGroupService(s.db)
		for _, g := range data.Groups {
			if _, err := grpSvc.Create(g.Name, g.Description, g.PermissionLevel); err != nil {
				return err
			}
		}
	}

	// OIDC providers are exported without secrets — log a note but skip import
	// (admin must re-enter client secrets after import)

	return nil
}
