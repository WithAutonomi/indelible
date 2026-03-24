package services

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"

	"github.com/WithAutonomi/indelible/internal/config"
)

// Notifier is the interface for sending transactional notifications.
// SMTP is the default implementation; webhook delivery can be added later.
type Notifier interface {
	SendPasswordReset(to, resetURL string) error
	SendEmailVerification(to, verifyURL string) error
}

// SMTPNotifier sends transactional emails via SMTP.
type SMTPNotifier struct {
	cfg config.SMTPConfig
}

func NewSMTPNotifier(cfg config.SMTPConfig) *SMTPNotifier {
	return &SMTPNotifier{cfg: cfg}
}

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

// WebhookNotifier is a stub for future webhook-based notification delivery.
// Companies that prefer routing notifications through their own systems
// (Slack, PagerDuty, custom HTTP endpoints) can use this instead of SMTP.
type WebhookNotifier struct {
	// webhookURL string
}

func NewWebhookNotifier() *WebhookNotifier {
	return &WebhookNotifier{}
}

func (w *WebhookNotifier) SendPasswordReset(to, resetURL string) error {
	slog.Warn("webhook notifier not implemented, password reset not delivered",
		"to", to, "reset_url", resetURL)
	return nil
}

func (w *WebhookNotifier) SendEmailVerification(to, verifyURL string) error {
	slog.Warn("webhook notifier not implemented, email verification not delivered",
		"to", to, "verify_url", verifyURL)
	return nil
}

// NoopNotifier does nothing — used when no notification method is configured.
// Logs a warning so operators know emails aren't being sent.
type NoopNotifier struct{}

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

// NewNotifier returns the appropriate notifier based on config.
func NewNotifier(cfg *config.Config) Notifier {
	if cfg.SMTPConfigured() {
		return NewSMTPNotifier(cfg.SMTP)
	}
	// TODO: check for webhook config and return WebhookNotifier
	return &NoopNotifier{}
}
