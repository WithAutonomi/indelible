package services

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookDeliveryLogAndGet(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh, err := whSvc.Create("https://example.com/hook", "generic", `["completed"]`)
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	delSvc.logDelivery(wh.ID, "completed", 200, true, 1, "")
	delSvc.logDelivery(wh.ID, "failed", 500, false, 3, "HTTP 500")

	log, err := delSvc.GetDeliveryLog(wh.ID, 10)
	if err != nil {
		t.Fatalf("GetDeliveryLog: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(log))
	}

	// Find each entry by event type (ordering may be non-deterministic within same second)
	var successEntry, failEntry *WebhookDelivery
	for _, entry := range log {
		if entry.EventType == "completed" {
			successEntry = entry
		}
		if entry.EventType == "failed" {
			failEntry = entry
		}
	}

	if successEntry == nil {
		t.Fatal("missing 'completed' entry")
	}
	if !successEntry.Success {
		t.Error("completed entry should have Success=true")
	}
	if successEntry.Attempts != 1 {
		t.Errorf("completed entry attempts = %d, want 1", successEntry.Attempts)
	}

	if failEntry == nil {
		t.Fatal("missing 'failed' entry")
	}
	if failEntry.Success {
		t.Error("failed entry should have Success=false")
	}
	if !failEntry.ErrorMessage.Valid || failEntry.ErrorMessage.String != "HTTP 500" {
		t.Errorf("error_message = %v", failEntry.ErrorMessage)
	}
	if failEntry.Attempts != 3 {
		t.Errorf("failed entry attempts = %d, want 3", failEntry.Attempts)
	}
}

func TestWebhookDeliveryLogDefaultLimit(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh, _ := whSvc.Create("https://example.com", "", "")

	// Insert 25 entries
	for i := 0; i < 25; i++ {
		delSvc.logDelivery(wh.ID, "completed", 200, true, 1, "")
	}

	// Passing 0 should default to 20
	log, err := delSvc.GetDeliveryLog(wh.ID, 0)
	if err != nil {
		t.Fatalf("GetDeliveryLog: %v", err)
	}
	if len(log) != 20 {
		t.Errorf("expected default limit 20, got %d", len(log))
	}
}

func TestWebhookDeliveryPrune_OldEntries(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh, _ := whSvc.Create("https://example.com", "", "")

	// Insert entries then backdate them to make them "old"
	delSvc.logDelivery(wh.ID, "completed", 200, true, 1, "")
	delSvc.logDelivery(wh.ID, "failed", 500, false, 1, "err")

	// Backdate to 48 hours ago
	old := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02 15:04:05")
	db.Exec(`UPDATE webhook_delivery_log SET created_at = ?`, old)

	// Prune entries older than 24 hours should remove both
	pruned, err := delSvc.PruneDeliveryLog(24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneDeliveryLog: %v", err)
	}
	if pruned != 2 {
		t.Errorf("expected 2 pruned, got %d", pruned)
	}

	log, _ := delSvc.GetDeliveryLog(wh.ID, 100)
	if len(log) != 0 {
		t.Errorf("expected 0 entries after prune, got %d", len(log))
	}
}

func TestWebhookDeliveryPrune_FreshEntries(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh, _ := whSvc.Create("https://example.com", "", "")

	delSvc.logDelivery(wh.ID, "completed", 200, true, 1, "")

	// Prune with very large retention -- 30 days. Fresh entries should survive.
	pruned, err := delSvc.PruneDeliveryLog(30 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneDeliveryLog: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned (entry is fresh), got %d", pruned)
	}
}

// TestWebhookDeadLetter_CaptureAndResend covers the V2-429 acceptance criteria:
// a forced delivery failure lands in the dead-letter store, an auth event is
// flagged, and the entry can be re-driven to resolution once the receiver
// recovers.
func TestWebhookDeadLetter_CaptureAndResend(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)
	delSvc.backoffBase = 0 // skip retry sleeps

	var down atomic.Bool
	down.Store(true)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, err := whSvc.Create(srv.URL, "generic", `["auth.password_reset_requested"]`)
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	// Exhaust retries against the failing receiver.
	delSvc.deliver(wh, WebhookPayload{
		EventType: "auth.password_reset_requested",
		Auth:      &WebhookAuthData{To: "user@example.com", URL: "https://app.example.com/reset?token=abc"},
	})
	if got := hits.Load(); got != 3 {
		t.Fatalf("expected 3 delivery attempts, got %d", got)
	}

	open, err := delSvc.ListDeadLetters(false, 50)
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("expected 1 dead-letter, got %d", len(open))
	}
	dl := open[0]
	if !dl.IsAuth {
		t.Error("auth event should set is_auth")
	}
	if dl.EventType != "auth.password_reset_requested" {
		t.Errorf("event_type = %q", dl.EventType)
	}
	if dl.WebhookURL != srv.URL {
		t.Errorf("webhook url = %q, want %q", dl.WebhookURL, srv.URL)
	}
	if dl.ResolvedAt.Valid {
		t.Error("fresh dead-letter should be unresolved")
	}

	// Receiver recovers; resend should succeed and resolve the entry.
	down.Store(false)
	if err := delSvc.Resend(dl.ID); err != nil {
		t.Fatalf("Resend: %v", err)
	}

	if openAfter, _ := delSvc.ListDeadLetters(false, 50); len(openAfter) != 0 {
		t.Errorf("expected 0 unresolved after resend, got %d", len(openAfter))
	}
	all, _ := delSvc.ListDeadLetters(true, 50)
	if len(all) != 1 {
		t.Fatalf("expected 1 entry including resolved, got %d", len(all))
	}
	if !all[0].ResolvedAt.Valid {
		t.Error("resent entry should be resolved")
	}
	if all[0].ResendCount != 1 {
		t.Errorf("resend_count = %d, want 1", all[0].ResendCount)
	}
}

// TestWebhookDeadLetter_ResendStillFailing verifies a resend against a still-down
// receiver returns an error and leaves the entry unresolved with bumped bookkeeping.
func TestWebhookDeadLetter_ResendStillFailing(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)
	delSvc.backoffBase = 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	wh, _ := whSvc.Create(srv.URL, "generic", `["completed"]`)
	delSvc.deliver(wh, WebhookPayload{EventType: "completed", Upload: &WebhookUploadData{UUID: "u1"}})

	open, _ := delSvc.ListDeadLetters(false, 50)
	if len(open) != 1 {
		t.Fatalf("expected 1 dead-letter, got %d", len(open))
	}
	if open[0].IsAuth {
		t.Error("non-auth event should not set is_auth")
	}

	if err := delSvc.Resend(open[0].ID); err == nil {
		t.Fatal("expected resend to fail against a down receiver")
	}

	stillOpen, _ := delSvc.ListDeadLetters(false, 50)
	if len(stillOpen) != 1 {
		t.Fatalf("entry should remain unresolved, got %d", len(stillOpen))
	}
	if stillOpen[0].ResendCount != 1 {
		t.Errorf("resend_count = %d, want 1", stillOpen[0].ResendCount)
	}
}

// TestWebhookDeadLetter_Dismiss verifies manual resolution.
func TestWebhookDeadLetter_Dismiss(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh, _ := whSvc.Create("https://example.com/hook", "generic", `["completed"]`)
	delSvc.recordDeadLetter(wh, WebhookPayload{EventType: "completed"}, 500, "HTTP 500")

	open, _ := delSvc.ListDeadLetters(false, 50)
	if len(open) != 1 {
		t.Fatalf("expected 1 dead-letter, got %d", len(open))
	}
	if err := delSvc.ResolveDeadLetter(open[0].ID); err != nil {
		t.Fatalf("ResolveDeadLetter: %v", err)
	}
	if remaining, _ := delSvc.ListDeadLetters(false, 50); len(remaining) != 0 {
		t.Errorf("expected 0 unresolved after dismiss, got %d", len(remaining))
	}

	// Dismissing a missing id returns ErrDeadLetterNotFound.
	if err := delSvc.ResolveDeadLetter(99999); err != ErrDeadLetterNotFound {
		t.Errorf("expected ErrDeadLetterNotFound, got %v", err)
	}
}

// TestWebhookDeadLetter_PruneResolvedOnly verifies prune removes only resolved
// rows older than the retention window and keeps unresolved ones.
func TestWebhookDeadLetter_PruneResolvedOnly(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh, _ := whSvc.Create("https://example.com/hook", "generic", `["completed"]`)
	delSvc.recordDeadLetter(wh, WebhookPayload{EventType: "completed"}, 500, "old-resolved")
	delSvc.recordDeadLetter(wh, WebhookPayload{EventType: "failed"}, 500, "old-unresolved")

	open, _ := delSvc.ListDeadLetters(false, 50)
	if len(open) != 2 {
		t.Fatalf("expected 2 dead-letters, got %d", len(open))
	}

	// Resolve the first, then backdate its resolved_at so it's outside the
	// retention window. PruneDeadLetters keys off resolved_at only; the
	// unresolved row keeps resolved_at NULL and must survive regardless of age.
	if err := delSvc.ResolveDeadLetter(open[0].ID); err != nil {
		t.Fatalf("ResolveDeadLetter: %v", err)
	}
	oldTS := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.Exec(`UPDATE webhook_dead_letter SET resolved_at = ? WHERE resolved_at IS NOT NULL`, oldTS); err != nil {
		t.Fatalf("backdate resolved_at: %v", err)
	}

	pruned, err := delSvc.PruneDeadLetters(24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneDeadLetters: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned (resolved only), got %d", pruned)
	}
	all, _ := delSvc.ListDeadLetters(true, 50)
	if len(all) != 1 {
		t.Errorf("expected 1 remaining (the unresolved one), got %d", len(all))
	}
}

func TestWebhookSubscribedTo(t *testing.T) {
	tests := []struct {
		events    string
		eventType string
		want      bool
	}{
		{`["completed","failed"]`, "completed", true},
		{`["completed","failed"]`, "failed", true},
		{`["completed","failed"]`, "processing", false},
		{`["completed"]`, "failed", false},
		{`[]`, "completed", false},
		{"invalid json", "completed", false},
	}

	for _, tc := range tests {
		wh := &Webhook{Events: tc.events}
		got := webhookSubscribedTo(wh, tc.eventType)
		if got != tc.want {
			t.Errorf("webhookSubscribedTo(%q, %q) = %v, want %v", tc.events, tc.eventType, got, tc.want)
		}
	}
}

func TestWebhookDeliveryLogByWebhook(t *testing.T) {
	db := setupTestDB(t)
	whSvc := NewWebhookService(db)
	delSvc := NewWebhookDeliveryService(db)

	wh1, _ := whSvc.Create("https://a.com", "", "")
	wh2, _ := whSvc.Create("https://b.com", "", "")

	delSvc.logDelivery(wh1.ID, "completed", 200, true, 1, "")
	delSvc.logDelivery(wh2.ID, "failed", 500, false, 1, "err")
	delSvc.logDelivery(wh2.ID, "completed", 200, true, 1, "")

	log1, _ := delSvc.GetDeliveryLog(wh1.ID, 100)
	if len(log1) != 1 {
		t.Errorf("wh1 should have 1 entry, got %d", len(log1))
	}

	log2, _ := delSvc.GetDeliveryLog(wh2.ID, 100)
	if len(log2) != 2 {
		t.Errorf("wh2 should have 2 entries, got %d", len(log2))
	}
}
