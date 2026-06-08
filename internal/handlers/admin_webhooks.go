package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
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

// @Summary      List all webhooks
// @Description  Return all configured webhook endpoints
// @Tags         Admin: Webhooks
// @Produce      json
// @Success      200 {object} map[string][]webhookResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks [get]
// @Security     BearerAuth
func AdminGetWebhooks(db *database.DB) http.HandlerFunc {
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

// @Summary      Create a webhook
// @Description  Register a new webhook endpoint for event notifications
// @Tags         Admin: Webhooks
// @Accept       json
// @Produce      json
// @Param        body body createWebhookRequest true "Webhook details"
// @Success      201 {object} webhookResponse
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks [post]
// @Security     BearerAuth
func AdminCreateWebhook(db *database.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)
	logSvc := services.NewLogService(db)

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
			if errors.Is(err, services.ErrInvalidURL) {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}
			jsonError(w, "failed to create webhook", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		// Redact the URL: webhook URLs can embed a secret in the path (e.g. Slack),
		// and the audit log is persisted in cleartext. Host is enough for forensics.
		auditEvent(r, logSvc, "webhook_created", "info", &callerID,
			fmt.Sprintf("id=%d url=%s integration=%s", webhook.ID, services.RedactWebhookURL(webhook.URL), webhook.IntegrationType))

		// Include secret in create response (shown once)
		resp := toWebhookResponse(webhook)
		jsonResponse(w, http.StatusCreated, map[string]any{
			"webhook": resp,
			"secret":  webhook.Secret,
		})
	}
}

// @Summary      Update a webhook
// @Description  Update an existing webhook's URL, events, integration type, or enabled status
// @Tags         Admin: Webhooks
// @Accept       json
// @Produce      json
// @Param        id   path int                  true "Webhook ID"
// @Param        body body updateWebhookRequest true "Updated webhook fields"
// @Success      200 {object} webhookResponse
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/{id} [put]
// @Security     BearerAuth
func AdminUpdateWebhook(db *database.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)
	logSvc := services.NewLogService(db)

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
			if errors.Is(err, services.ErrInvalidURL) {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}
			jsonError(w, "failed to update webhook", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "webhook_updated", "info", &callerID, fmt.Sprintf("id=%d", id))

		jsonResponse(w, http.StatusOK, toWebhookResponse(webhook))
	}
}

// @Summary      Delete a webhook
// @Description  Remove a webhook endpoint
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        id path int true "Webhook ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/{id} [delete]
// @Security     BearerAuth
func AdminDeleteWebhook(db *database.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)
	logSvc := services.NewLogService(db)

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

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "webhook_deleted", "info", &callerID, fmt.Sprintf("id=%d", id))

		jsonResponse(w, http.StatusOK, map[string]string{"message": "webhook deleted"})
	}
}

// @Summary      Test a webhook
// @Description  Send a test ping to a webhook endpoint and return the result
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        id path int true "Webhook ID"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/{id}/test [post]
// @Security     BearerAuth
// AdminTestWebhook sends a test ping to a webhook endpoint synchronously.
func AdminTestWebhook(db *database.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)
	deliverySvc := services.NewWebhookDeliveryService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid webhook id", http.StatusBadRequest)
			return
		}

		webhook, err := webhookSvc.GetByID(id)
		if err != nil {
			if errors.Is(err, services.ErrWebhookNotFound) {
				jsonError(w, "webhook not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to get webhook", http.StatusInternalServerError)
			return
		}

		statusCode, success, testErr := deliverySvc.FireTestPing(webhook)
		resp := map[string]any{
			"success":     success,
			"status_code": statusCode,
		}
		if testErr != nil {
			resp["error"] = testErr.Error()
		}

		jsonResponse(w, http.StatusOK, resp)
	}
}

type deliveryResponse struct {
	ID           int64  `json:"id"`
	WebhookID    int64  `json:"webhook_id"`
	EventType    string `json:"event_type"`
	StatusCode   *int64 `json:"status_code"`
	Success      bool   `json:"success"`
	Attempts     int    `json:"attempts"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// @Summary      List webhook deliveries
// @Description  Return recent delivery log entries for a specific webhook
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        id    path  int true  "Webhook ID"
// @Param        limit query int false "Max results (default 20, max 100)"
// @Success      200 {object} map[string][]deliveryResponse
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/{id}/deliveries [get]
// @Security     BearerAuth
// AdminGetWebhookDeliveries returns recent delivery log entries for a webhook.
func AdminGetWebhookDeliveries(db *database.DB) http.HandlerFunc {
	deliverySvc := services.NewWebhookDeliveryService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid webhook id", http.StatusBadRequest)
			return
		}

		limit := 20
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}

		deliveries, err := deliverySvc.GetDeliveryLog(id, limit)
		if err != nil {
			jsonError(w, "failed to get delivery log", http.StatusInternalServerError)
			return
		}

		resp := make([]deliveryResponse, 0, len(deliveries))
		for _, d := range deliveries {
			dr := deliveryResponse{
				ID:        d.ID,
				WebhookID: d.WebhookID,
				EventType: d.EventType,
				Success:   d.Success,
				Attempts:  d.Attempts,
				CreatedAt: d.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
			if d.StatusCode.Valid {
				dr.StatusCode = &d.StatusCode.Int64
			}
			if d.ErrorMessage.Valid {
				dr.ErrorMessage = d.ErrorMessage.String
			}
			resp = append(resp, dr)
		}

		jsonResponse(w, http.StatusOK, map[string]any{"deliveries": resp})
	}
}

type deadLetterResponse struct {
	ID             int64  `json:"id"`
	WebhookID      int64  `json:"webhook_id"`
	WebhookURL     string `json:"webhook_url"`
	EventType      string `json:"event_type"`
	LastStatusCode *int64 `json:"last_status_code"`
	LastError      string `json:"last_error,omitempty"`
	Attempts       int    `json:"attempts"`
	ResendCount    int    `json:"resend_count"`
	IsAuth         bool   `json:"is_auth"`
	CreatedAt      string `json:"created_at"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
}

// @Summary      List webhook dead-letters
// @Description  Return webhook deliveries that exhausted every retry. By default only unresolved (still-actionable) entries are returned. Payloads are never included (they can carry one-time recovery links).
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        include_resolved query bool false "Include resolved/dismissed entries"
// @Param        limit            query int  false "Max results (default 50, max 200)"
// @Success      200 {object} map[string][]deadLetterResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/dead-letters [get]
// @Security     BearerAuth
// AdminGetWebhookDeadLetters returns the dead-letter queue across all webhooks.
func AdminGetWebhookDeadLetters(db *database.DB) http.HandlerFunc {
	deliverySvc := services.NewWebhookDeliveryService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		includeResolved := r.URL.Query().Get("include_resolved") == "true"
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}

		entries, err := deliverySvc.ListDeadLetters(includeResolved, limit)
		if err != nil {
			jsonError(w, "failed to list dead-letters", http.StatusInternalServerError)
			return
		}

		resp := make([]deadLetterResponse, 0, len(entries))
		for _, d := range entries {
			dr := deadLetterResponse{
				ID:          d.ID,
				WebhookID:   d.WebhookID,
				WebhookURL:  d.WebhookURL,
				EventType:   d.EventType,
				Attempts:    d.Attempts,
				ResendCount: d.ResendCount,
				IsAuth:      d.IsAuth,
				CreatedAt:   d.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
			if d.LastStatusCode.Valid {
				dr.LastStatusCode = &d.LastStatusCode.Int64
			}
			if d.LastError.Valid {
				dr.LastError = d.LastError.String
			}
			if d.ResolvedAt.Valid {
				dr.ResolvedAt = d.ResolvedAt.Time.Format("2006-01-02T15:04:05Z")
			}
			resp = append(resp, dr)
		}

		jsonResponse(w, http.StatusOK, map[string]any{"dead_letters": resp})
	}
}

// @Summary      Resend a dead-lettered delivery
// @Description  Re-drive a webhook delivery that previously exhausted its retries. On success the entry is marked resolved.
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        id path int true "Dead-letter entry ID"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      502 {object} map[string]string
// @Router       /admin/webhooks/dead-letters/{id}/resend [post]
// @Security     BearerAuth
// AdminResendWebhookDeadLetter re-drives a single dead-lettered delivery.
func AdminResendWebhookDeadLetter(db *database.DB) http.HandlerFunc {
	deliverySvc := services.NewWebhookDeliveryService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid dead-letter id", http.StatusBadRequest)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		if err := deliverySvc.Resend(id); err != nil {
			if errors.Is(err, services.ErrDeadLetterNotFound) {
				jsonError(w, "dead-letter entry not found", http.StatusNotFound)
				return
			}
			auditEvent(r, logSvc, "webhook_dead_letter_resend_failed", "warning", &callerID,
				fmt.Sprintf("id=%d error=%s", id, err.Error()))
			// Delivery itself failed again (receiver still down) — surface as 502.
			jsonResponse(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error()})
			return
		}

		auditEvent(r, logSvc, "webhook_dead_letter_resent", "info", &callerID, fmt.Sprintf("id=%d", id))
		jsonResponse(w, http.StatusOK, map[string]any{"success": true})
	}
}

// @Summary      Dismiss a dead-lettered delivery
// @Description  Mark a dead-letter entry resolved without re-driving it (e.g. the link has expired or was delivered out-of-band).
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        id path int true "Dead-letter entry ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/dead-letters/{id} [delete]
// @Security     BearerAuth
// AdminDismissWebhookDeadLetter marks a dead-letter entry resolved.
func AdminDismissWebhookDeadLetter(db *database.DB) http.HandlerFunc {
	deliverySvc := services.NewWebhookDeliveryService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid dead-letter id", http.StatusBadRequest)
			return
		}

		if err := deliverySvc.ResolveDeadLetter(id); err != nil {
			if errors.Is(err, services.ErrDeadLetterNotFound) {
				jsonError(w, "dead-letter entry not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to dismiss dead-letter", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "webhook_dead_letter_dismissed", "info", &callerID, fmt.Sprintf("id=%d", id))
		jsonResponse(w, http.StatusOK, map[string]string{"message": "dead-letter dismissed"})
	}
}

// @Summary      Rotate webhook secret
// @Description  Generate a new signing secret for a webhook endpoint
// @Tags         Admin: Webhooks
// @Produce      json
// @Param        id path int true "Webhook ID"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/webhooks/{id}/rotate-secret [post]
// @Security     BearerAuth
func AdminRotateWebhookSecret(db *database.DB) http.HandlerFunc {
	webhookSvc := services.NewWebhookService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid webhook id", http.StatusBadRequest)
			return
		}

		secret, err := webhookSvc.RotateSecret(id)
		if err != nil {
			if errors.Is(err, services.ErrWebhookNotFound) {
				jsonError(w, "webhook not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to rotate secret", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{"secret": secret})
	}
}
