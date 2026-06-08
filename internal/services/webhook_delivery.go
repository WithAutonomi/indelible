package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

// escapeSlackText escapes the characters Slack treats as mrkdwn control
// characters in a text span, so user-supplied values can't inject formatting or
// fake links into a message.
func escapeSlackText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// RedactWebhookURL returns at most scheme://host for logging. The path/query is
// dropped because webhook URLs commonly embed their credential there (e.g. a
// Slack webhook's secret lives in the trailing path segment), and webhook logs
// flow to stdout / aggregators / SIEM. The webhook_id is logged alongside for
// correlation, so the full URL is never needed in logs.
func RedactWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "redacted"
	}
	return u.Scheme + "://" + u.Host
}

// WebhookPayload is the JSON payload sent to webhook endpoints.
type WebhookPayload struct {
	EventType  string                 `json:"event_type"`
	Timestamp  string                 `json:"timestamp"`
	Upload     *WebhookUploadData     `json:"upload,omitempty"`
	System     *WebhookSystemData     `json:"system,omitempty"`
	Tags       *WebhookTagData        `json:"tags,omitempty"`
	Collection *WebhookCollectionData `json:"collection,omitempty"`
	Auth       *WebhookAuthData       `json:"auth,omitempty"`
}

// WebhookAuthData carries the link the recipient must click for password reset
// or email verification. Receiving systems are expected to deliver `url` to
// `to` via their preferred channel (Slack DM, transactional email API, etc.).
type WebhookAuthData struct {
	To  string `json:"to"`
	URL string `json:"url"`
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

// WebhookDeadLetter is a delivery that exhausted every retry. The full payload
// is kept so an operator can re-drive it. WebhookURL is populated from a join
// for display and is empty when loaded without one.
type WebhookDeadLetter struct {
	ID             int64
	WebhookID      int64
	WebhookURL     string
	EventType      string
	Payload        string
	LastStatusCode sql.NullInt64
	LastError      sql.NullString
	Attempts       int
	ResendCount    int
	IsAuth         bool
	CreatedAt      time.Time
	ResolvedAt     sql.NullTime
}

// ErrDeadLetterNotFound is returned when a dead-letter row id does not exist.
var ErrDeadLetterNotFound = errors.New("dead-letter entry not found")

// WebhookDeliveryService handles dispatching webhook notifications.
type WebhookDeliveryService struct {
	db         *database.DB
	webhookSvc *WebhookService
	client     *http.Client
	// backoffBase is the unit of the exponential retry backoff between delivery
	// attempts (sleep = backoffBase << attempt). Defaults to 1s; tests set it to
	// 0 to avoid the multi-second waits when exercising the retry-exhaustion path.
	backoffBase time.Duration
}

// NewWebhookDeliveryService creates a new delivery service. The HTTP client is
// SSRF-guarded — webhook URLs are admin-supplied (and become user-supplied once
// per-user webhooks ship), so deliveries must not reach internal/metadata hosts.
func NewWebhookDeliveryService(db *database.DB) *WebhookDeliveryService {
	return &WebhookDeliveryService{
		db:          db,
		webhookSvc:  NewWebhookService(db),
		client:      newGuardedHTTPClient(5 * time.Second),
		backoffBase: time.Second,
	}
}

// setSignedHeaders sets the common webhook request headers and, when the webhook
// has a signing secret, an HMAC over "timestamp.body" (Stripe-style). Binding the
// timestamp into the signature lets receivers reject replays; the same timestamp
// is emitted in X-Webhook-Timestamp for verification.
func setSignedHeaders(req *http.Request, wh *Webhook, body []byte) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Indelible-Webhook/2.0")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Webhook-Timestamp", ts)
	if wh.Secret != "" {
		mac := hmac.New(sha256.New, []byte(wh.Secret))
		mac.Write([]byte(ts))
		mac.Write([]byte("."))
		mac.Write(body)
		req.Header.Set("X-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
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

// FireAuthEvent dispatches an auth.* notification (password reset, email
// verification) to every enabled webhook subscribed to the event. The handler
// for each webhook is responsible for actually delivering the link to the user
// — we just relay the request through the signed webhook pipeline.
func (s *WebhookDeliveryService) FireAuthEvent(eventType string, data *WebhookAuthData) {
	webhooks, err := s.webhookSvc.GetEnabled()
	if err != nil || len(webhooks) == 0 {
		slog.Warn("no enabled webhooks to deliver auth event",
			"event", eventType, "to", data.To)
		return
	}

	payload := WebhookPayload{
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Auth:      data,
	}

	delivered := 0
	for _, wh := range webhooks {
		if !webhookSubscribedTo(wh, eventType) {
			continue
		}
		go s.deliver(wh, payload)
		delivered++
	}
	if delivered == 0 {
		slog.Warn("no webhook subscribed to auth event; user will not receive their link",
			"event", eventType, "to", data.To)
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
	setSignedHeaders(req, wh, body)

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
			time.Sleep(s.backoffBase << uint(attempt))
		}

		req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
		if err != nil {
			slog.Error("webhook request error", "webhook_id", wh.ID, "error", err)
			s.logDelivery(wh.ID, payload.EventType, 0, false, attempt+1, err.Error())
			return
		}
		setSignedHeaders(req, wh, body)

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err.Error()
			slog.Warn("webhook delivery failed", "webhook_id", wh.ID, "attempt", attempt+1, "error", err)
			continue
		}
		resp.Body.Close()
		lastStatusCode = resp.StatusCode

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			slog.Debug("webhook delivered", "webhook_id", wh.ID, "host", RedactWebhookURL(wh.URL), "status", resp.StatusCode)
			s.logDelivery(wh.ID, payload.EventType, resp.StatusCode, true, attempt+1, "")
			return
		}
		lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode)
		slog.Warn("webhook non-2xx response", "webhook_id", wh.ID, "status", resp.StatusCode, "attempt", attempt+1)
	}

	slog.Error("webhook delivery exhausted retries", "webhook_id", wh.ID, "host", RedactWebhookURL(wh.URL))
	s.logDelivery(wh.ID, payload.EventType, lastStatusCode, false, 3, lastErr)
	s.recordDeadLetter(wh, payload, lastStatusCode, lastErr)
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

// recordDeadLetter persists a delivery that exhausted every retry so it can be
// re-driven later. Auth events (recovery links) are additionally escalated to the
// system log because their loss is directly user-facing.
func (s *WebhookDeliveryService) recordDeadLetter(wh *Webhook, payload WebhookPayload, lastStatusCode int, lastErr string) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook dead-letter marshal failed", "webhook_id", wh.ID, "error", err)
		return
	}
	var sc sql.NullInt64
	if lastStatusCode > 0 {
		sc = sql.NullInt64{Int64: int64(lastStatusCode), Valid: true}
	}
	var le sql.NullString
	if lastErr != "" {
		le = sql.NullString{String: lastErr, Valid: true}
	}
	isAuth := strings.HasPrefix(payload.EventType, "auth.")

	_, err = s.db.Exec(
		`INSERT INTO webhook_dead_letter (webhook_id, event_type, payload, last_status_code, last_error, attempts, is_auth)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		wh.ID, payload.EventType, string(body), sc, le, 3, isAuth,
	)
	if err != nil {
		slog.Error("failed to record webhook dead-letter", "webhook_id", wh.ID, "error", err)
		return
	}

	if isAuth {
		to := ""
		if payload.Auth != nil {
			to = payload.Auth.To
		}
		slog.Error("auth webhook delivery failed — user recovery link dead-lettered",
			"webhook_id", wh.ID, "event", payload.EventType, "to", to, "error", lastErr)
		// Surface in the system log so operators see the failure without polling
		// the dead-letter queue. Best-effort: a logging failure must not mask the
		// (already-persisted) dead-letter row.
		if err := NewLogService(s.db).WriteSystem("error", "webhook",
			fmt.Sprintf("auth notification delivery failed (%s) — recovery link queued in dead-letter", payload.EventType),
			fmt.Sprintf("webhook_id=%d to=%s last_error=%s", wh.ID, to, lastErr), ""); err != nil {
			slog.Error("failed to escalate auth webhook failure to system log", "webhook_id", wh.ID, "error", err)
		}
	}
}

// ListDeadLetters returns dead-letter entries across all webhooks, newest first.
// When includeResolved is false only unresolved (still-actionable) rows are
// returned — the operator queue. The webhook URL is joined for display.
func (s *WebhookDeliveryService) ListDeadLetters(includeResolved bool, limit int) ([]*WebhookDeadLetter, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT d.id, d.webhook_id, w.url, d.event_type, d.payload, d.last_status_code, d.last_error,
	             d.attempts, d.resend_count, d.is_auth, d.created_at, d.resolved_at
	      FROM webhook_dead_letter d JOIN webhook_config w ON w.id = d.webhook_id`
	if !includeResolved {
		q += ` WHERE d.resolved_at IS NULL`
	}
	q += ` ORDER BY d.created_at DESC LIMIT ?`

	rows, err := s.db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*WebhookDeadLetter
	for rows.Next() {
		d := &WebhookDeadLetter{}
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.WebhookURL, &d.EventType, &d.Payload,
			&d.LastStatusCode, &d.LastError, &d.Attempts, &d.ResendCount, &d.IsAuth,
			&d.CreatedAt, &d.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// getDeadLetter loads a single dead-letter row by id.
func (s *WebhookDeliveryService) getDeadLetter(id int64) (*WebhookDeadLetter, error) {
	d := &WebhookDeadLetter{}
	err := s.db.QueryRow(
		`SELECT id, webhook_id, event_type, payload, last_status_code, last_error,
		        attempts, resend_count, is_auth, created_at, resolved_at
		 FROM webhook_dead_letter WHERE id = ?`, id,
	).Scan(&d.ID, &d.WebhookID, &d.EventType, &d.Payload, &d.LastStatusCode, &d.LastError,
		&d.Attempts, &d.ResendCount, &d.IsAuth, &d.CreatedAt, &d.ResolvedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeadLetterNotFound
		}
		return nil, err
	}
	return d, nil
}

// Resend re-drives a dead-lettered delivery once, synchronously, through the
// SSRF-guarded client. The stored payload is re-formatted with the webhook's
// current integration type (it may have changed since the original attempt). On
// a 2xx the row is marked resolved; otherwise its resend bookkeeping is updated
// and an error is returned. Every resend is recorded in the delivery log.
func (s *WebhookDeliveryService) Resend(id int64) error {
	dl, err := s.getDeadLetter(id)
	if err != nil {
		return err
	}
	wh, err := s.webhookSvc.GetByID(dl.WebhookID)
	if err != nil {
		return err
	}

	var payload WebhookPayload
	if err := json.Unmarshal([]byte(dl.Payload), &payload); err != nil {
		return fmt.Errorf("decode dead-letter payload: %w", err)
	}
	body, err := s.formatPayload(wh.IntegrationType, payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	setSignedHeaders(req, wh, body)

	statusCode := 0
	errMsg := ""
	resp, derr := s.client.Do(req)
	if derr != nil {
		errMsg = derr.Error()
	} else {
		statusCode = resp.StatusCode
		resp.Body.Close()
		if statusCode < 200 || statusCode >= 300 {
			errMsg = fmt.Sprintf("HTTP %d", statusCode)
		}
	}
	ok := derr == nil && statusCode >= 200 && statusCode < 300

	// Record the manual resend in the delivery log for the audit trail.
	s.logDelivery(wh.ID, payload.EventType, statusCode, ok, dl.ResendCount+1, errMsg)

	if ok {
		_, e := s.db.Exec(
			`UPDATE webhook_dead_letter SET resolved_at = CURRENT_TIMESTAMP, resend_count = resend_count + 1,
			        last_status_code = ?, last_error = NULL WHERE id = ?`,
			sql.NullInt64{Int64: int64(statusCode), Valid: true}, id,
		)
		return e
	}

	var sc sql.NullInt64
	if statusCode > 0 {
		sc = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	if _, e := s.db.Exec(
		`UPDATE webhook_dead_letter SET resend_count = resend_count + 1, last_status_code = ?, last_error = ? WHERE id = ?`,
		sc, sql.NullString{String: errMsg, Valid: errMsg != ""}, id,
	); e != nil {
		slog.Error("failed to update dead-letter after failed resend", "dead_letter_id", id, "error", e)
	}
	return fmt.Errorf("resend failed: %s", errMsg)
}

// ResolveDeadLetter marks a dead-letter entry resolved without re-driving it
// (operator dismissal — e.g. the link has expired or was delivered out-of-band).
func (s *WebhookDeliveryService) ResolveDeadLetter(id int64) error {
	result, err := s.db.Exec(
		`UPDATE webhook_dead_letter SET resolved_at = CURRENT_TIMESTAMP WHERE id = ? AND resolved_at IS NULL`, id,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		// Either it does not exist or it is already resolved; distinguish for the
		// handler so a missing id maps to 404.
		if _, gErr := s.getDeadLetter(id); gErr != nil {
			return gErr
		}
	}
	return nil
}

// PruneDeadLetters removes resolved dead-letter rows older than the retention
// window. Unresolved rows are kept regardless of age so a still-actionable
// recovery link is never silently dropped.
func (s *WebhookDeliveryService) PruneDeadLetters(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(
		`DELETE FROM webhook_dead_letter WHERE resolved_at IS NOT NULL AND resolved_at < ?`, cutoff,
	)
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

	// User-controlled values (filenames, error messages, recipient, names) are
	// escaped so they can't inject Slack mrkdwn control characters into the
	// message. Slack requires escaping &, <, > in text spans.
	switch {
	case payload.Upload != nil:
		text = fmt.Sprintf("*%s*: `%s` — %s (%d bytes)",
			payload.EventType, escapeSlackText(payload.Upload.Filename), escapeSlackText(payload.Upload.Status), payload.Upload.FileSize)
		if payload.Upload.ActualCost != nil {
			text += fmt.Sprintf(" | Cost: %s atto", escapeSlackText(*payload.Upload.ActualCost))
		}
		if payload.Upload.ErrorMessage != nil {
			text += fmt.Sprintf("\nError: %s", escapeSlackText(*payload.Upload.ErrorMessage))
		}
	case payload.Tags != nil:
		text = fmt.Sprintf("*%s*: `%s` — %d tags", payload.EventType, escapeSlackText(payload.Tags.UploadUUID), len(payload.Tags.Tags))
	case payload.Collection != nil:
		text = fmt.Sprintf("*%s*: `%s` — collection `%s`", payload.EventType, escapeSlackText(payload.Collection.UploadUUID), escapeSlackText(payload.Collection.CollectionName))
	case payload.System != nil:
		text = fmt.Sprintf("*%s*: %s (%.1f%%)", payload.EventType, escapeSlackText(payload.System.Message), payload.System.Value)
	case payload.Auth != nil:
		// Slack mrkdwn link format: <url|label>. The recipient address is sent
		// verbatim (escaped) — the Slack channel operator needs it to identify
		// who the link belongs to.
		text = fmt.Sprintf("*%s*\nDeliver to: `%s`\n<%s|Click here to continue>",
			payload.EventType, escapeSlackText(payload.Auth.To), payload.Auth.URL)
	default:
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
