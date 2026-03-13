package services

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrWebhookNotFound = errors.New("webhook not found")
)

// Webhook represents a webhook configuration.
type Webhook struct {
	ID              int64
	URL             string
	IntegrationType string // "generic" or "slack"
	IsEnabled       bool
	Events          string // JSON array e.g. '["completed","failed"]'
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// WebhookService handles webhook configuration CRUD.
type WebhookService struct {
	db *sql.DB
}

// NewWebhookService creates a new WebhookService.
func NewWebhookService(db *sql.DB) *WebhookService {
	return &WebhookService{db: db}
}

// Create adds a new webhook configuration.
func (s *WebhookService) Create(url, integrationType, events string) (*Webhook, error) {
	if integrationType == "" {
		integrationType = "generic"
	}
	if events == "" {
		events = `["completed","failed"]`
	}

	result, err := s.db.Exec(
		`INSERT INTO webhook_config (url, integration_type, events) VALUES (?, ?, ?)`,
		url, integrationType, events,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return s.GetByID(id)
}

// GetByID retrieves a webhook by ID.
func (s *WebhookService) GetByID(id int64) (*Webhook, error) {
	w := &Webhook{}
	err := s.db.QueryRow(
		`SELECT id, url, integration_type, is_enabled, events, created_at, updated_at FROM webhook_config WHERE id = ?`, id,
	).Scan(&w.ID, &w.URL, &w.IntegrationType, &w.IsEnabled, &w.Events, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}
	return w, nil
}

// List returns all webhook configurations.
func (s *WebhookService) List() ([]*Webhook, error) {
	rows, err := s.db.Query(
		`SELECT id, url, integration_type, is_enabled, events, created_at, updated_at FROM webhook_config ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		w := &Webhook{}
		if err := rows.Scan(&w.ID, &w.URL, &w.IntegrationType, &w.IsEnabled, &w.Events, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	return webhooks, rows.Err()
}

// Update modifies a webhook configuration.
func (s *WebhookService) Update(id int64, url, integrationType, events string, isEnabled bool) (*Webhook, error) {
	_, err := s.db.Exec(
		`UPDATE webhook_config SET url = ?, integration_type = ?, events = ?, is_enabled = ?, updated_at = datetime('now') WHERE id = ?`,
		url, integrationType, events, isEnabled, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

// Delete removes a webhook configuration.
func (s *WebhookService) Delete(id int64) error {
	result, err := s.db.Exec(`DELETE FROM webhook_config WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrWebhookNotFound
	}
	return nil
}

// GetEnabled returns all enabled webhooks (for delivery).
func (s *WebhookService) GetEnabled() ([]*Webhook, error) {
	rows, err := s.db.Query(
		`SELECT id, url, integration_type, is_enabled, events, created_at, updated_at FROM webhook_config WHERE is_enabled = 1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		w := &Webhook{}
		if err := rows.Scan(&w.ID, &w.URL, &w.IntegrationType, &w.IsEnabled, &w.Events, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	return webhooks, rows.Err()
}
