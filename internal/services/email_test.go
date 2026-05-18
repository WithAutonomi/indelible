package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
)

// --- Method() identifies the active channel ----------------------------------

func TestNotifierMethodNames(t *testing.T) {
	if got := (&NoopNotifier{}).Method(); got != NotifierNoop {
		t.Errorf("Noop.Method = %q, want %q", got, NotifierNoop)
	}
	if got := NewSMTPNotifier(config.SMTPConfig{Host: "x", From: "y"}).Method(); got != NotifierSMTP {
		t.Errorf("SMTP.Method = %q, want %q", got, NotifierSMTP)
	}
	if got := NewWebhookNotifier(setupTestDB(t)).Method(); got != NotifierWebhook {
		t.Errorf("Webhook.Method = %q, want %q", got, NotifierWebhook)
	}
}

// --- resolveNotifierMethod dispatch matrix -----------------------------------

func TestResolveNotifierMethod_AutoPrefersSMTP(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{SMTP: config.SMTPConfig{Host: "mx.acme.com", From: "no-reply@acme.com"}}
	if got := resolveNotifierMethod(cfg, db); got != NotifierSMTP {
		t.Errorf("auto with SMTP configured = %q, want smtp", got)
	}
}

func TestResolveNotifierMethod_AutoFallsBackToWebhook(t *testing.T) {
	db := setupTestDB(t)
	// No SMTP, but a webhook subscribed to an auth event exists.
	whSvc := NewWebhookService(db)
	if _, err := whSvc.Create("https://example.com/hook", "generic",
		`["`+EventAuthPasswordResetRequested+`"]`); err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	cfg := &config.Config{}
	if got := resolveNotifierMethod(cfg, db); got != NotifierWebhook {
		t.Errorf("auto with auth webhook = %q, want webhook", got)
	}
}

func TestResolveNotifierMethod_AutoFallsBackToNoop(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{}
	if got := resolveNotifierMethod(cfg, db); got != NotifierNoop {
		t.Errorf("auto with nothing configured = %q, want noop", got)
	}
}

func TestResolveNotifierMethod_ExplicitSMTPMissingConfig(t *testing.T) {
	db := setupTestDB(t)
	// Setting forces SMTP, but no SMTP config → fall back to noop with warning.
	settings := NewSettingsService(db)
	if err := settings.Update(map[string]string{"notifier_method": "smtp"}, createTestUser(t, NewUserService(db), "admin@x.com", "A", "U").ID, "", ""); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	cfg := &config.Config{}
	if got := resolveNotifierMethod(cfg, db); got != NotifierNoop {
		t.Errorf("explicit smtp without config = %q, want noop", got)
	}
}

func TestResolveNotifierMethod_ExplicitWebhookHonoredEvenWithoutSubscriber(t *testing.T) {
	db := setupTestDB(t)
	// "webhook" honours the operator's intent even if no webhook subscribes —
	// the dispatch-time warning will surface the misconfiguration.
	settings := NewSettingsService(db)
	if err := settings.Update(map[string]string{"notifier_method": "webhook"}, createTestUser(t, NewUserService(db), "admin@x.com", "A", "U").ID, "", ""); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	cfg := &config.Config{SMTP: config.SMTPConfig{Host: "mx", From: "x"}} // SMTP configured but ignored
	if got := resolveNotifierMethod(cfg, db); got != NotifierWebhook {
		t.Errorf("explicit webhook = %q, want webhook", got)
	}
}

func TestResolveNotifierMethod_ExplicitNoop(t *testing.T) {
	db := setupTestDB(t)
	settings := NewSettingsService(db)
	if err := settings.Update(map[string]string{"notifier_method": "noop"}, createTestUser(t, NewUserService(db), "admin@x.com", "A", "U").ID, "", ""); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	cfg := &config.Config{SMTP: config.SMTPConfig{Host: "mx", From: "x"}}
	if got := resolveNotifierMethod(cfg, db); got != NotifierNoop {
		t.Errorf("explicit noop = %q, want noop", got)
	}
}

func TestNotifierMethodValidator_RejectsUnknown(t *testing.T) {
	db := setupTestDB(t)
	settings := NewSettingsService(db)
	admin := createTestUser(t, NewUserService(db), "admin@x.com", "A", "U")
	err := settings.Update(map[string]string{"notifier_method": "carrier-pigeon"}, admin.ID, "", "")
	if err == nil {
		t.Fatal("expected validation error for unknown notifier method")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("expected *ValidationError, got %T", err)
	}
}

// --- WebhookNotifier dispatches signed payloads ------------------------------

type capturedRequest struct {
	headers http.Header
	body    []byte
}

func TestWebhookNotifier_FiresSignedPasswordResetEvent(t *testing.T) {
	db := setupTestDB(t)

	// Spin up a fake receiver and stash whatever it gets.
	got := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- capturedRequest{headers: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	whSvc := NewWebhookService(db)
	wh, err := whSvc.Create(server.URL, "generic",
		`["`+EventAuthPasswordResetRequested+`"]`)
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	// Set a secret so we can validate the HMAC.
	const secret = "test-shared-secret-please-rotate"
	if _, err := db.Exec(`UPDATE webhook_config SET secret = ? WHERE id = ?`, secret, wh.ID); err != nil {
		t.Fatalf("set secret: %v", err)
	}

	notifier := NewWebhookNotifier(db)
	if err := notifier.SendPasswordReset("alice@example.com", "https://app/reset?token=xyz"); err != nil {
		t.Fatalf("SendPasswordReset: %v", err)
	}

	var req capturedRequest
	select {
	case req = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook receiver was never called")
	}

	// Payload shape.
	var payload WebhookPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v\nbody: %s", err, req.body)
	}
	if payload.EventType != EventAuthPasswordResetRequested {
		t.Errorf("event_type = %q", payload.EventType)
	}
	if payload.Auth == nil {
		t.Fatal("payload.Auth is nil")
	}
	if payload.Auth.To != "alice@example.com" {
		t.Errorf("auth.to = %q", payload.Auth.To)
	}
	if payload.Auth.URL != "https://app/reset?token=xyz" {
		t.Errorf("auth.url = %q", payload.Auth.URL)
	}

	// HMAC-SHA256 signature header.
	sigHeader := req.headers.Get("X-Signature-256")
	if sigHeader == "" {
		t.Fatal("missing X-Signature-256 header")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(req.body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sigHeader != expected {
		t.Errorf("signature mismatch:\n got %q\nwant %q", sigHeader, expected)
	}
}

func TestWebhookNotifier_NoSubscriberIsNoop(t *testing.T) {
	// With no webhook subscribed to the event, SendPasswordReset returns nil
	// and logs a warning — we just assert no panic / no error.
	db := setupTestDB(t)
	notifier := NewWebhookNotifier(db)
	if err := notifier.SendPasswordReset("bob@x.com", "https://app/reset?t=1"); err != nil {
		t.Errorf("expected nil err with no subscribers, got %v", err)
	}
}

// --- FireAuthEvent only delivers to subscribed webhooks ----------------------

func TestFireAuthEvent_SkipsUnsubscribedWebhooks(t *testing.T) {
	db := setupTestDB(t)

	var mu sync.Mutex
	hitURLs := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hitURLs = append(hitURLs, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	whSvc := NewWebhookService(db)
	if _, err := whSvc.Create(server.URL+"/subscribed", "generic",
		`["`+EventAuthEmailVerificationRequested+`"]`); err != nil {
		t.Fatalf("create subscribed: %v", err)
	}
	if _, err := whSvc.Create(server.URL+"/unsubscribed", "generic",
		`["upload.completed"]`); err != nil {
		t.Fatalf("create unsubscribed: %v", err)
	}

	NewWebhookDeliveryService(db).FireAuthEvent(
		EventAuthEmailVerificationRequested,
		&WebhookAuthData{To: "c@x.com", URL: "https://verify"},
	)

	// FireAuthEvent dispatches async via goroutines; give them a moment.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(hitURLs)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hitURLs) != 1 || hitURLs[0] != "/subscribed" {
		t.Errorf("expected only /subscribed to be hit, got %v", hitURLs)
	}
}

// --- NewNotifier integrates everything ---------------------------------------

func TestNewNotifier_RespectsSetting(t *testing.T) {
	db := setupTestDB(t)
	settings := NewSettingsService(db)

	cfg := &config.Config{SMTP: config.SMTPConfig{Host: "mx", From: "x"}}

	if got := NewNotifier(cfg, db).Method(); got != NotifierSMTP {
		t.Errorf("default with SMTP cfg = %q, want smtp", got)
	}

	if err := settings.Update(map[string]string{"notifier_method": "noop"}, createTestUser(t, NewUserService(db), "admin@x.com", "A", "U").ID, "", ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := NewNotifier(cfg, db).Method(); got != NotifierNoop {
		t.Errorf("after setting noop = %q, want noop", got)
	}
}
