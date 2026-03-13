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
func (s *SettingsService) Update(changes map[string]string, userID int64, ipAddress, userAgent string) error {
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
			`INSERT INTO settings (key, value, updated_at, updated_by) VALUES (?, ?, datetime('now'), ?)
			 ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = datetime('now'), updated_by = ?`,
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

// Export returns all settings as a JSON byte slice.
func (s *SettingsService) Export() ([]byte, error) {
	settings, err := s.GetAll()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(settings, "", "  ")
}

// Import replaces settings from a JSON map, logging all changes.
func (s *SettingsService) Import(data map[string]string, userID int64, ipAddress, userAgent string) error {
	return s.Update(data, userID, ipAddress, userAgent)
}
