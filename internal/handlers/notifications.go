package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type notificationPrefResponse struct {
	WebhookURL *string `json:"webhook_url"`
	Events     string  `json:"events"`
	DigestMode string  `json:"digest_mode"`
}

type updateNotificationPrefRequest struct {
	WebhookURL *string `json:"webhook_url"`
	Events     string  `json:"events"`      // JSON array string
	DigestMode string  `json:"digest_mode"` // "realtime", "daily", "weekly"
}

// GetNotificationPrefs godoc
// @Summary Get notification preferences
// @Tags Notifications
// @Produce json
// @Success 200 {object} notificationPrefResponse
// @Router /notifications/preferences [get]
// @Security BearerAuth
func GetNotificationPrefs(db *database.DB) http.HandlerFunc {
	prefSvc := services.NewNotificationPrefService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		pref, err := prefSvc.Get(userID)
		if err != nil {
			jsonError(w, "failed to get notification preferences", http.StatusInternalServerError)
			return
		}

		resp := notificationPrefResponse{
			Events:     pref.Events,
			DigestMode: pref.DigestMode,
		}
		if pref.WebhookURL.Valid {
			resp.WebhookURL = &pref.WebhookURL.String
		}

		jsonResponse(w, http.StatusOK, resp)
	}
}

// UpdateNotificationPrefs sets the authenticated user's notification preferences.
// UpdateNotificationPrefs godoc
// @Summary Update notification preferences
// @Tags Notifications
// @Accept json
// @Produce json
// @Success 200 {object} notificationPrefResponse
// @Router /notifications/preferences [put]
// @Security BearerAuth
func UpdateNotificationPrefs(db *database.DB) http.HandlerFunc {
	prefSvc := services.NewNotificationPrefService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var req updateNotificationPrefRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate digest mode
		if req.DigestMode != "" && req.DigestMode != "realtime" && req.DigestMode != "daily" && req.DigestMode != "weekly" {
			jsonError(w, "digest_mode must be realtime, daily, or weekly", http.StatusBadRequest)
			return
		}

		pref, err := prefSvc.Update(userID, req.WebhookURL, req.Events, req.DigestMode)
		if err != nil {
			jsonError(w, "failed to update notification preferences", http.StatusInternalServerError)
			return
		}

		resp := notificationPrefResponse{
			Events:     pref.Events,
			DigestMode: pref.DigestMode,
		}
		if pref.WebhookURL.Valid {
			resp.WebhookURL = &pref.WebhookURL.String
		}

		jsonResponse(w, http.StatusOK, resp)
	}
}
