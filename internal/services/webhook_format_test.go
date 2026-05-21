package services

import (
	"encoding/json"
	"strings"
	"testing"
)

// V2-323: formatSlack must surface every payload variant. The bug it fixes
// was a missing Auth branch that fell through to the test-ping default,
// silently dropping password-reset URLs.

func unmarshalSlackText(t *testing.T, b []byte) string {
	t.Helper()
	var msg map[string]any
	if err := json.Unmarshal(b, &msg); err != nil {
		t.Fatalf("unmarshal slack message: %v", err)
	}
	text, _ := msg["text"].(string)
	return text
}

func TestFormatSlack_AuthIncludesRecipientAndURL(t *testing.T) {
	s := &WebhookDeliveryService{}
	payload := WebhookPayload{
		EventType: "auth.password_reset_requested",
		Timestamp: "2026-05-20T10:00:00Z",
		Auth: &WebhookAuthData{
			To:  "user@example.com",
			URL: "https://indelible.example/reset?token=abc123",
		},
	}

	out, err := s.formatSlack(payload)
	if err != nil {
		t.Fatalf("formatSlack: %v", err)
	}
	text := unmarshalSlackText(t, out)

	if !strings.Contains(text, "user@example.com") {
		t.Errorf("slack text missing recipient: %q", text)
	}
	if !strings.Contains(text, "https://indelible.example/reset?token=abc123") {
		t.Errorf("slack text missing URL: %q", text)
	}
	if strings.Contains(text, "test ping") {
		t.Errorf("slack text fell through to default test-ping branch: %q", text)
	}
}

func TestFormatSlack_TagsIncludesUploadAndCount(t *testing.T) {
	s := &WebhookDeliveryService{}
	payload := WebhookPayload{
		EventType: "tags.changed",
		Tags: &WebhookTagData{
			UploadUUID: "upload-abc",
			Tags: map[string][]string{
				"environment": {"prod"},
				"team":        {"data", "platform"},
			},
		},
	}

	text := unmarshalSlackText(t, mustFormat(t, s, payload))
	if !strings.Contains(text, "upload-abc") {
		t.Errorf("slack text missing upload UUID: %q", text)
	}
}

func TestFormatSlack_CollectionIncludesName(t *testing.T) {
	s := &WebhookDeliveryService{}
	payload := WebhookPayload{
		EventType: "collection.changed",
		Collection: &WebhookCollectionData{
			UploadUUID:     "upload-xyz",
			CollectionID:   42,
			CollectionName: "Q2-Reports",
		},
	}

	text := unmarshalSlackText(t, mustFormat(t, s, payload))
	if !strings.Contains(text, "Q2-Reports") {
		t.Errorf("slack text missing collection name: %q", text)
	}
	if !strings.Contains(text, "upload-xyz") {
		t.Errorf("slack text missing upload UUID: %q", text)
	}
}

func TestFormatSlack_DefaultPingStillWorksForBareEvent(t *testing.T) {
	// Sanity check that the new Auth case didn't shadow the default ping path
	// for events with no payload (e.g. test_ping).
	s := &WebhookDeliveryService{}
	payload := WebhookPayload{
		EventType: "test_ping",
		Timestamp: "2026-05-20T10:00:00Z",
	}
	text := unmarshalSlackText(t, mustFormat(t, s, payload))
	if !strings.Contains(text, "test ping") {
		t.Errorf("test_ping should still hit default branch: %q", text)
	}
}

func mustFormat(t *testing.T, s *WebhookDeliveryService, payload WebhookPayload) []byte {
	t.Helper()
	out, err := s.formatSlack(payload)
	if err != nil {
		t.Fatalf("formatSlack: %v", err)
	}
	return out
}
