package services

import (
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
