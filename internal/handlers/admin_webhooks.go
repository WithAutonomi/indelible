package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/maidsafe/indelible/internal/services"
)

type webhookResponse struct {
	ID              int64  `json:"id"`
	URL             string `json:"url"`
	IntegrationType string `json:"integration_type"`
	IsEnabled       bool   `json:"is_enabled"`
	Events          string `json:"events"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func toWebhookResponse(w *services.Webhook) webhookResponse {
	return webhookResponse{
		ID:              w.ID,
		URL:             w.URL,
		IntegrationType: w.IntegrationType,
		IsEnabled:       w.IsEnabled,
		Events:          w.Events,
		CreatedAt:       w.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:       w.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

type createWebhookRequest struct {
	URL             string `json:"url"`
	IntegrationType string `json:"integration_type"`
	Events          string `json:"events"` // JSON array string
}

type updateWebhookRequest struct {
	URL             string `json:"url"`
	IntegrationType string `json:"integration_type"`
	Events          string `json:"events"`
	IsEnabled       bool   `json:"is_enabled"`
}

func AdminGetWebhooks(db *sql.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		webhooks, err := webhookSvc.List()
		if err != nil {
			jsonError(w, "failed to list webhooks", http.StatusInternalServerError)
			return
		}

		resp := make([]webhookResponse, 0, len(webhooks))
		for _, wh := range webhooks {
			resp = append(resp, toWebhookResponse(wh))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"webhooks": resp})
	}
}

func AdminCreateWebhook(db *sql.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createWebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			jsonError(w, "url is required", http.StatusBadRequest)
			return
		}

		webhook, err := webhookSvc.Create(req.URL, req.IntegrationType, req.Events)
		if err != nil {
			jsonError(w, "failed to create webhook", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, toWebhookResponse(webhook))
	}
}

func AdminUpdateWebhook(db *sql.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid webhook id", http.StatusBadRequest)
			return
		}

		var req updateWebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		webhook, err := webhookSvc.Update(id, req.URL, req.IntegrationType, req.Events, req.IsEnabled)
		if err != nil {
			if errors.Is(err, services.ErrWebhookNotFound) {
				jsonError(w, "webhook not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to update webhook", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, toWebhookResponse(webhook))
	}
}

func AdminDeleteWebhook(db *sql.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid webhook id", http.StatusBadRequest)
			return
		}

		if err := webhookSvc.Delete(id); err != nil {
			if errors.Is(err, services.ErrWebhookNotFound) {
				jsonError(w, "webhook not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to delete webhook", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "webhook deleted"})
	}
}
