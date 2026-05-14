package services

import (
	"database/sql"
	"errors"
	"time"
)

// NotificationPref represents a user's notification preferences.
type NotificationPref struct {
	ID         int64
	UserID     int64
	WebhookURL sql.NullString
	Events     string // JSON array
	DigestMode string // "realtime", "daily", "weekly"
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NotificationPrefService handles user notification preferences.
type NotificationPrefService struct {
	db *sql.DB
}

// NewNotificationPrefService creates a new NotificationPrefService.
func NewNotificationPrefService(db *sql.DB) *NotificationPrefService {
	return &NotificationPrefService{db: db}
}

// Get returns a user's notification preferences. Creates default if none exist.
func (s *NotificationPrefService) Get(userID int64) (*NotificationPref, error) {
	pref := &NotificationPref{}
	err := s.db.QueryRow(
		`SELECT id, user_id, webhook_url, events, digest_mode, created_at, updated_at FROM user_notification_prefs WHERE user_id = ?`, userID,
	).Scan(&pref.ID, &pref.UserID, &pref.WebhookURL, &pref.Events, &pref.DigestMode, &pref.CreatedAt, &pref.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return default (empty) preferences
			return &NotificationPref{
				UserID:     userID,
				Events:     "[]",
				DigestMode: "realtime",
			}, nil
		}
		return nil, err
	}
	return pref, nil
}

// Update sets a user's notification preferences (upsert).
func (s *NotificationPrefService) Update(userID int64, webhookURL *string, events, digestMode string) (*NotificationPref, error) {
	var url sql.NullString
	if webhookURL != nil && *webhookURL != "" {
		url = sql.NullString{String: *webhookURL, Valid: true}
	}
	if events == "" {
		events = "[]"
	}
	if digestMode == "" {
		digestMode = "realtime"
	}

	_, err := s.db.Exec(
		`INSERT INTO user_notification_prefs (user_id, webhook_url, events, digest_mode) VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET webhook_url = ?, events = ?, digest_mode = ?, updated_at = CURRENT_TIMESTAMP`,
		userID, url, events, digestMode, url, events, digestMode,
	)
	if err != nil {
		return nil, err
	}

	return s.Get(userID)
}
