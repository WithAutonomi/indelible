package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// V2-317: every audit_log row produced by an HTTP request must carry the
// chi-generated (or client-supplied) X-Request-Id so an operator can pivot
// between the audit table, system_log, and slog stdout for one request.

func TestRequestID_FlowsFromHeaderToAuditRow(t *testing.T) {
	env := setupSCIMTest(t)

	// Send a SCIM create with an explicit X-Request-Id so we know what to
	// look for in the audit table. chi.RequestID respects the inbound header.
	body, _ := json.Marshal(scimUserBody("flow@test.com", "Flow", "Test", "ext-flow", true))
	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewReader(body))
	req.Header.Set("Content-Type", scimContentType)
	req.Header.Set("Authorization", "Bearer "+env.scimSecret)
	req.Header.Set("X-Request-Id", "req-flow-fixture-001")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: got %d, body: %s", w.Code, w.Body.String())
	}

	// chi should echo the same request ID back on the response.
	if got := w.Header().Get("X-Request-Id"); got != "req-flow-fixture-001" {
		t.Errorf("X-Request-Id response header = %q, want req-flow-fixture-001", got)
	}

	// The SCIM create handler runs h.audit which writes WriteAudit with the
	// chi request ID. Pull the audit log via the admin API and confirm.
	req2 := httptest.NewRequest("GET", "/api/v2/admin/logs/audit?event_type=scim.user.create", nil)
	req2.Header.Set("Authorization", "Bearer "+env.adminToken)
	w2 := httptest.NewRecorder()
	env.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("query audit logs: got %d, body: %s", w2.Code, w2.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	entries := resp["entries"].([]any)
	if len(entries) == 0 {
		t.Fatal("no audit entries — scim.user.create event was not written")
	}
	row := entries[0].(map[string]any)
	if got := row["request_id"].(string); got != "req-flow-fixture-001" {
		t.Errorf("audit row request_id = %q, want req-flow-fixture-001", got)
	}
}

func TestRequestID_ResponseHeaderSetEvenWithoutClientID(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-Id"); got == "" {
		t.Error("X-Request-Id should be set even when client didn't send one (chi auto-generates)")
	}
}
