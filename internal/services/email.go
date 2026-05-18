package services

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
)

// Notifier sends transactional notifications. Method names a delivery channel
// so /health and admin tooling can surface which path is actually live.
type Notifier interface {
	SendPasswordReset(to, resetURL string) error
	SendEmailVerification(to, verifyURL string) error
	Method() NotifierMethodName
}

// NotifierMethodName enumerates the delivery channels. Stored as the
// "notifier_method" setting and surfaced on /health.
type NotifierMethodName string

const (
	NotifierSMTP    NotifierMethodName = "smtp"
	NotifierWebhook NotifierMethodName = "webhook"
	NotifierNoop    NotifierMethodName = "noop"
	NotifierAuto    NotifierMethodName = "auto"
)

// SMTPNotifier sends transactional emails via SMTP.
type SMTPNotifier struct {
	cfg config.SMTPConfig
}

func NewSMTPNotifier(cfg config.SMTPConfig) *SMTPNotifier {
	return &SMTPNotifier{cfg: cfg}
}

func (s *SMTPNotifier) Method() NotifierMethodName { return NotifierSMTP }

func (s *SMTPNotifier) SendPasswordReset(to, resetURL string) error {
	subject := "Indelible — Password Reset"
	body := fmt.Sprintf(
		"You requested a password reset.\n\nClick the link below to reset your password (valid for 1 hour):\n\n%s\n\nIf you did not request this, ignore this email.",
		resetURL,
	)
	return s.send(to, subject, body)
}

func (s *SMTPNotifier) SendEmailVerification(to, verifyURL string) error {
	subject := "Indelible — Verify Your Email"
	body := fmt.Sprintf(
		"Please verify your email address by clicking the link below:\n\n%s\n\nIf you did not create an account, ignore this email.",
		verifyURL,
	)
	return s.send(to, subject, body)
}

func (s *SMTPNotifier) send(to, subject, body string) error {
	msg := strings.Join([]string{
		"From: " + s.cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	return smtp.SendMail(addr, auth, s.cfg.From, []string{to}, []byte(msg))
}

// Auth event types emitted by WebhookNotifier. Consumers subscribe to these to
// route password-reset and email-verification messages through their own
// transactional channel (Slack, custom relay, Postmark, etc.).
const (
	EventAuthPasswordResetRequested     = "auth.password_reset_requested"
	EventAuthEmailVerificationRequested = "auth.email_verification_requested"
)

// AuthEventTypes returns the list of webhook events the email subsystem emits.
// Used by the admin UI to populate the webhook subscription checklist.
func AuthEventTypes() []string {
	return []string{
		EventAuthPasswordResetRequested,
		EventAuthEmailVerificationRequested,
	}
}

// WebhookNotifier routes transactional emails through the existing webhook
// delivery pipeline (HMAC-SHA256 signed, 3-attempt exponential backoff). At
// least one enabled webhook must subscribe to the relevant auth event type;
// otherwise the delivery is a no-op with a logged warning so operators can
// notice misconfiguration.
type WebhookNotifier struct {
	deliverySvc *WebhookDeliveryService
}

func NewWebhookNotifier(db *database.DB) *WebhookNotifier {
	return &WebhookNotifier{deliverySvc: NewWebhookDeliveryService(db)}
}

func (w *WebhookNotifier) Method() NotifierMethodName { return NotifierWebhook }

func (w *WebhookNotifier) SendPasswordReset(to, resetURL string) error {
	w.deliverySvc.FireAuthEvent(EventAuthPasswordResetRequested, &WebhookAuthData{
		To:  to,
		URL: resetURL,
	})
	return nil
}

func (w *WebhookNotifier) SendEmailVerification(to, verifyURL string) error {
	w.deliverySvc.FireAuthEvent(EventAuthEmailVerificationRequested, &WebhookAuthData{
		To:  to,
		URL: verifyURL,
	})
	return nil
}

// NoopNotifier does nothing — used when no delivery method is configured.
// Logs a warning so operators know emails aren't being sent. Boot-time code
// should ERROR-log when this is the active notifier; see NewNotifier.
type NoopNotifier struct{}

func (n *NoopNotifier) Method() NotifierMethodName { return NotifierNoop }

func (n *NoopNotifier) SendPasswordReset(to, resetURL string) error {
	slog.Warn("no notifier configured, password reset not delivered",
		"to", to, "reset_url", resetURL)
	return nil
}

func (n *NoopNotifier) SendEmailVerification(to, verifyURL string) error {
	slog.Warn("no notifier configured, email verification not delivered",
		"to", to, "verify_url", verifyURL)
	return nil
}

// NewNotifier returns the appropriate notifier based on config + the
// admin-selected "notifier_method" setting.
//
//   - "smtp"    → SMTPNotifier (falls back to Noop with a warning if SMTP isn't configured)
//   - "webhook" → WebhookNotifier (warns at boot if no enabled webhook subscribes to auth events)
//   - "noop"    → NoopNotifier (intentional opt-out, useful for local dev)
//   - "auto"    → SMTP if configured, else webhook if any enabled webhook subscribes to an auth event, else noop
//
// Empty string is treated as "auto" for backwards compatibility.
func NewNotifier(cfg *config.Config, db *database.DB) Notifier {
	method := resolveNotifierMethod(cfg, db)
	switch method {
	case NotifierSMTP:
		return NewSMTPNotifier(cfg.SMTP)
	case NotifierWebhook:
		return NewWebhookNotifier(db)
	default:
		return &NoopNotifier{}
	}
}

func resolveNotifierMethod(cfg *config.Config, db *database.DB) NotifierMethodName {
	requested := NotifierAuto
	if db != nil {
		settings := NewSettingsService(db)
		if v, _ := settings.Get("notifier_method"); v != "" {
			requested = NotifierMethodName(v)
		}
	}

	switch requested {
	case NotifierSMTP:
		if cfg.SMTPConfigured() {
			return NotifierSMTP
		}
		slog.Warn("notifier_method=smtp but SMTP is not configured; falling back to noop")
		return NotifierNoop
	case NotifierWebhook:
		return NotifierWebhook
	case NotifierNoop:
		return NotifierNoop
	case NotifierAuto, "":
		if cfg.SMTPConfigured() {
			return NotifierSMTP
		}
		if db != nil && webhookSubscribedToAnyAuthEvent(db) {
			return NotifierWebhook
		}
		return NotifierNoop
	default:
		slog.Warn("unknown notifier_method, falling back to auto", "value", string(requested))
		if cfg.SMTPConfigured() {
			return NotifierSMTP
		}
		return NotifierNoop
	}
}

// webhookSubscribedToAnyAuthEvent reports whether at least one enabled webhook
// is subscribed to one of the auth.* event types. Used by "auto" resolution
// to decide if webhook delivery is actually viable.
func webhookSubscribedToAnyAuthEvent(db *database.DB) bool {
	whSvc := NewWebhookService(db)
	enabled, err := whSvc.GetEnabled()
	if err != nil {
		return false
	}
	for _, wh := range enabled {
		for _, evt := range AuthEventTypes() {
			if webhookSubscribedTo(wh, evt) {
				return true
			}
		}
	}
	return false
}

// LogStartupNotifierStatus emits a boot-time signal about which notifier is
// active. ERROR when Noop is in use so operators stop being surprised by silent
// password-reset failures in production.
func LogStartupNotifierStatus(n Notifier) {
	switch n.Method() {
	case NotifierSMTP:
		slog.Info("notifier active", "method", NotifierSMTP)
	case NotifierWebhook:
		slog.Info("notifier active", "method", NotifierWebhook)
	case NotifierNoop:
		slog.Error("notifier is NOOP — password reset and email verification will not be delivered. " +
			"Configure SMTP, or enable a webhook subscribed to auth.* events, or set notifier_method explicitly.")
	}
}
