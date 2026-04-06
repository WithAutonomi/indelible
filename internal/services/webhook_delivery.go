package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// WebhookPayload is the JSON payload sent to webhook endpoints.
type WebhookPayload struct {
	EventType  string                   `json:"event_type"`
	Timestamp  string                   `json:"timestamp"`
	Upload     *WebhookUploadData       `json:"upload,omitempty"`
	System     *WebhookSystemData       `json:"system,omitempty"`
	Tags       *WebhookTagData          `json:"tags,omitempty"`
	Collection *WebhookCollectionData   `json:"collection,omitempty"`
}

// WebhookTagData contains tag change details in webhook payloads.
type WebhookTagData struct {
	UploadUUID string              `json:"upload_uuid"`
	Tags       map[string][]string `json:"tags"`
}

// WebhookCollectionData contains collection membership change details in webhook payloads.
type WebhookCollectionData struct {
	UploadUUID     string `json:"upload_uuid"`
	CollectionID   int64  `json:"collection_id"`
	CollectionName string `json:"collection_name"`
}

// WebhookUploadData contains upload details in webhook payloads.
type WebhookUploadData struct {
	UUID         string  `json:"uuid"`
	UserID       int64   `json:"user_id"`
	TokenID      *int64  `json:"token_id,omitempty"`
	Filename     string  `json:"filename"`
	Status       string  `json:"status"`
	FileSize     int64   `json:"file_size"`
	Visibility   string  `json:"visibility"`
	ActualCost   *string `json:"actual_cost,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
}

// WebhookSystemData contains system alert details in webhook payloads.
type WebhookSystemData struct {
	AlertType string  `json:"alert_type"`
	Message   string  `json:"message"`
	Value     float64 `json:"value"`
}

// WebhookDelivery represents a logged delivery attempt.
type WebhookDelivery struct {
	ID           int64
	WebhookID    int64
	EventType    string
	StatusCode   sql.NullInt64
	Success      bool
	Attempts     int
	ErrorMessage sql.NullString
	CreatedAt    time.Time
}

// WebhookDeliveryService handles dispatching webhook notifications.
type WebhookDeliveryService struct {
	db         *sql.DB
	webhookSvc *WebhookService
	client     *http.Client
}

// NewWebhookDeliveryService creates a new delivery service.
func NewWebhookDeliveryService(db *sql.DB) *WebhookDeliveryService {
	return &WebhookDeliveryService{
		db:         db,
		webhookSvc: NewWebhookService(db),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// webhookSubscribedTo checks if a webhook is subscribed to the given event type.
func webhookSubscribedTo(wh *Webhook, eventType string) bool {
	var events []string
	if err := json.Unmarshal([]byte(wh.Events), &events); err != nil {
		return false
	}
	for _, e := range events {
		if e == eventType {
			return true
		}
	}
	return false
}

// FireUploadEvent sends webhook notifications for an upload status change.
func (s *WebhookDeliveryService) FireUploadEvent(eventType string, upload *Upload) {
	webhooks, err := s.webhookSvc.GetEnabled()
	if err != nil || len(webhooks) == 0 {
		return
	}

	uploadData := &WebhookUploadData{
		UUID:       upload.UUID,
		UserID:     upload.UserID,
		Filename:   upload.OriginalFilename,
		Status:     upload.Status,
		FileSize:   upload.FileSize,
		Visibility: upload.Visibility,
	}
	if upload.TokenID.Valid {
		tid := upload.TokenID.Int64
		uploadData.TokenID = &tid
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
		if !webhookSubscribedTo(wh, eventType) {
			continue
		}
		go s.deliver(wh, payload)
	}
}

// FireSystemEvent sends webhook notifications for system-level events.
func (s *WebhookDeliveryService) FireSystemEvent(eventType string, data *WebhookSystemData) {
	webhooks, err := s.webhookSvc.GetEnabled()
	if err != nil || len(webhooks) == 0 {
		return
	}

	payload := WebhookPayload{
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		System:    data,
	}

	for _, wh := range webhooks {
		if !webhookSubscribedTo(wh, eventType) {
			continue
		}
		go s.deliver(wh, payload)
	}
}

// FireTagEvent sends webhook notifications when tags change on an upload.
func (s *WebhookDeliveryService) FireTagEvent(eventType string, uploadUUID string, tags map[string][]string) {
	webhooks, err := s.webhookSvc.GetEnabled()
	if err != nil || len(webhooks) == 0 {
		return
	}

	payload := WebhookPayload{
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Tags:      &WebhookTagData{UploadUUID: uploadUUID, Tags: tags},
	}

	for _, wh := range webhooks {
		if !webhookSubscribedTo(wh, eventType) {
			continue
		}
		go s.deliver(wh, payload)
	}
}

// FireCollectionEvent sends webhook notifications when collection membership changes.
func (s *WebhookDeliveryService) FireCollectionEvent(eventType string, uploadUUID string, collectionID int64, collectionName string) {
	webhooks, err := s.webhookSvc.GetEnabled()
	if err != nil || len(webhooks) == 0 {
		return
	}

	payload := WebhookPayload{
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Collection: &WebhookCollectionData{
			UploadUUID:     uploadUUID,
			CollectionID:   collectionID,
			CollectionName: collectionName,
		},
	}

	for _, wh := range webhooks {
		if !webhookSubscribedTo(wh, eventType) {
			continue
		}
		go s.deliver(wh, payload)
	}
}

// FireTestPing sends a test ping synchronously and returns the result.
func (s *WebhookDeliveryService) FireTestPing(wh *Webhook) (statusCode int, success bool, err error) {
	payload := WebhookPayload{
		EventType: "test_ping",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, err := s.formatPayload(wh.IntegrationType, payload)
	if err != nil {
		s.logDelivery(wh.ID, "test_ping", 0, false, 1, err.Error())
		return 0, false, err
	}

	req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
	if err != nil {
		s.logDelivery(wh.ID, "test_ping", 0, false, 1, err.Error())
		return 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Indelible-Webhook/2.0")
	req.Header.Set("X-Webhook-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	if wh.Secret != "" {
		mac := hmac.New(sha256.New, []byte(wh.Secret))
		mac.Write(body)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Signature-256", sig)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.logDelivery(wh.ID, "test_ping", 0, false, 1, err.Error())
		return 0, false, err
	}
	resp.Body.Close()

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	errMsg := ""
	if !ok {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	s.logDelivery(wh.ID, "test_ping", resp.StatusCode, ok, 1, errMsg)
	return resp.StatusCode, ok, nil
}

func (s *WebhookDeliveryService) deliver(wh *Webhook, payload WebhookPayload) {
	body, err := s.formatPayload(wh.IntegrationType, payload)
	if err != nil {
		slog.Error("webhook format error", "webhook_id", wh.ID, "error", err)
		s.logDelivery(wh.ID, payload.EventType, 0, false, 0, err.Error())
		return
	}

	var lastStatusCode int
	var lastErr string

	// 3 retry attempts with exponential backoff
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}

		req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
		if err != nil {
			slog.Error("webhook request error", "webhook_id", wh.ID, "error", err)
			s.logDelivery(wh.ID, payload.EventType, 0, false, attempt+1, err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Indelible-Webhook/2.0")
		req.Header.Set("X-Webhook-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

		// HMAC-SHA256 signature if webhook has a signing secret
		if wh.Secret != "" {
			mac := hmac.New(sha256.New, []byte(wh.Secret))
			mac.Write(body)
			sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-Signature-256", sig)
		}

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err.Error()
			slog.Warn("webhook delivery failed", "webhook_id", wh.ID, "attempt", attempt+1, "error", err)
			continue
		}
		resp.Body.Close()
		lastStatusCode = resp.StatusCode

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			slog.Debug("webhook delivered", "webhook_id", wh.ID, "url", wh.URL, "status", resp.StatusCode)
			s.logDelivery(wh.ID, payload.EventType, resp.StatusCode, true, attempt+1, "")
			return
		}
		lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode)
		slog.Warn("webhook non-2xx response", "webhook_id", wh.ID, "status", resp.StatusCode, "attempt", attempt+1)
	}

	slog.Error("webhook delivery exhausted retries", "webhook_id", wh.ID, "url", wh.URL)
	s.logDelivery(wh.ID, payload.EventType, lastStatusCode, false, 3, lastErr)
}

// logDelivery records a delivery attempt in the database.
func (s *WebhookDeliveryService) logDelivery(webhookID int64, eventType string, statusCode int, success bool, attempts int, errMsg string) {
	var sc sql.NullInt64
	if statusCode > 0 {
		sc = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	var em sql.NullString
	if errMsg != "" {
		em = sql.NullString{String: errMsg, Valid: true}
	}

	_, err := s.db.Exec(
		`INSERT INTO webhook_delivery_log (webhook_id, event_type, status_code, success, attempts, error_message) VALUES (?, ?, ?, ?, ?, ?)`,
		webhookID, eventType, sc, success, attempts, em,
	)
	if err != nil {
		slog.Error("failed to log webhook delivery", "webhook_id", webhookID, "error", err)
	}
}

// GetDeliveryLog returns recent delivery log entries for a webhook.
func (s *WebhookDeliveryService) GetDeliveryLog(webhookID int64, limit int) ([]*WebhookDelivery, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, webhook_id, event_type, status_code, success, attempts, error_message, created_at
		 FROM webhook_delivery_log WHERE webhook_id = ? ORDER BY created_at DESC LIMIT ?`,
		webhookID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*WebhookDelivery
	for rows.Next() {
		d := &WebhookDelivery{}
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &d.StatusCode, &d.Success, &d.Attempts, &d.ErrorMessage, &d.CreatedAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// PruneDeliveryLog removes delivery log entries older than the given duration.
func (s *WebhookDeliveryService) PruneDeliveryLog(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(`DELETE FROM webhook_delivery_log WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *WebhookDeliveryService) formatPayload(integrationType string, payload WebhookPayload) ([]byte, error) {
	if integrationType == "slack" {
		return s.formatSlack(payload)
	}
	return json.Marshal(payload)
}

func (s *WebhookDeliveryService) formatSlack(payload WebhookPayload) ([]byte, error) {
	var text string

	if payload.Upload != nil {
		text = fmt.Sprintf("*%s*: `%s` — %s (%d bytes)",
			payload.EventType, payload.Upload.Filename, payload.Upload.Status, payload.Upload.FileSize)
		if payload.Upload.ActualCost != nil {
			text += fmt.Sprintf(" | Cost: %s atto", *payload.Upload.ActualCost)
		}
		if payload.Upload.ErrorMessage != nil {
			text += fmt.Sprintf("\nError: %s", *payload.Upload.ErrorMessage)
		}
	} else if payload.Tags != nil {
		text = fmt.Sprintf("*%s*: `%s` — %d tags", payload.EventType, payload.Tags.UploadUUID, len(payload.Tags.Tags))
	} else if payload.Collection != nil {
		text = fmt.Sprintf("*%s*: `%s` — collection `%s`", payload.EventType, payload.Collection.UploadUUID, payload.Collection.CollectionName)
	} else if payload.System != nil {
		text = fmt.Sprintf("*%s*: %s (%.1f%%)", payload.EventType, payload.System.Message, payload.System.Value)
	} else {
		text = fmt.Sprintf("*%s* — Indelible test ping at %s", payload.EventType, payload.Timestamp)
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
