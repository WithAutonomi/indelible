package services

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// WebhookPayload is the generic JSON payload sent to webhook endpoints.
type WebhookPayload struct {
	EventType string    `json:"event_type"`
	Timestamp string    `json:"timestamp"`
	Upload    *WebhookUploadData `json:"upload,omitempty"`
}

// WebhookUploadData contains upload details in webhook payloads.
type WebhookUploadData struct {
	UUID             string  `json:"uuid"`
	Filename         string  `json:"filename"`
	Status           string  `json:"status"`
	FileSize         int64   `json:"file_size"`
	Visibility       string  `json:"visibility"`
	ActualCost       *string `json:"actual_cost,omitempty"`
	ErrorMessage     *string `json:"error_message,omitempty"`
}

// WebhookDeliveryService handles dispatching webhook notifications.
type WebhookDeliveryService struct {
	webhookSvc *WebhookService
	client     *http.Client
}

// NewWebhookDeliveryService creates a new delivery service.
func NewWebhookDeliveryService(db *sql.DB) *WebhookDeliveryService {
	return &WebhookDeliveryService{
		webhookSvc: NewWebhookService(db),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// FireUploadEvent sends webhook notifications for an upload status change.
func (s *WebhookDeliveryService) FireUploadEvent(eventType string, upload *Upload) {
	webhooks, err := s.webhookSvc.GetEnabled()
	if err != nil || len(webhooks) == 0 {
		return
	}

	uploadData := &WebhookUploadData{
		UUID:       upload.UUID,
		Filename:   upload.OriginalFilename,
		Status:     upload.Status,
		FileSize:   upload.FileSize,
		Visibility: upload.Visibility,
	}
	if upload.ActualCost.Valid {
		uploadData.ActualCost = &upload.ActualCost.String
	}
	if upload.ErrorMessage.Valid {
		uploadData.ErrorMessage = &upload.ErrorMessage.String
	}

	payload := WebhookPayload{
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Upload:    uploadData,
	}

	for _, wh := range webhooks {
		go s.deliver(wh, payload)
	}
}

func (s *WebhookDeliveryService) deliver(wh *Webhook, payload WebhookPayload) {
	body, err := s.formatPayload(wh.IntegrationType, payload)
	if err != nil {
		slog.Error("webhook format error", "webhook_id", wh.ID, "error", err)
		return
	}

	// 3 retry attempts with exponential backoff
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}

		req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
		if err != nil {
			slog.Error("webhook request error", "webhook_id", wh.ID, "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Indelible-Webhook/2.0")

		resp, err := s.client.Do(req)
		if err != nil {
			slog.Warn("webhook delivery failed", "webhook_id", wh.ID, "attempt", attempt+1, "error", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			slog.Debug("webhook delivered", "webhook_id", wh.ID, "url", wh.URL, "status", resp.StatusCode)
			return
		}
		slog.Warn("webhook non-2xx response", "webhook_id", wh.ID, "status", resp.StatusCode, "attempt", attempt+1)
	}

	slog.Error("webhook delivery exhausted retries", "webhook_id", wh.ID, "url", wh.URL)
}

func (s *WebhookDeliveryService) formatPayload(integrationType string, payload WebhookPayload) ([]byte, error) {
	if integrationType == "slack" {
		return s.formatSlack(payload)
	}
	return json.Marshal(payload)
}

func (s *WebhookDeliveryService) formatSlack(payload WebhookPayload) ([]byte, error) {
	text := fmt.Sprintf("*%s*: `%s` — %s (%d bytes)",
		payload.EventType, payload.Upload.Filename, payload.Upload.Status, payload.Upload.FileSize)
	if payload.Upload.ActualCost != nil {
		text += fmt.Sprintf(" | Cost: %s atto", *payload.Upload.ActualCost)
	}
	if payload.Upload.ErrorMessage != nil {
		text += fmt.Sprintf("\nError: %s", *payload.Upload.ErrorMessage)
	}

	slackMsg := map[string]any{
		"text": text,
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": text,
				},
			},
		},
	}
	return json.Marshal(slackMsg)
}
