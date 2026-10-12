package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
)

// TestBillingRelay_GatewayErrorBodyNotEchoed proves the relay forwards a
// gateway refusal's status and short error string only — never the raw body
// (#163 review, V2-1269).
func TestBillingRelay_GatewayErrorBodyNotEchoed(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"amount below minimum","internal":"stack: topup.go:42 ledger=pg://secret"}`))
	}))
	defer gw.Close()
	cfg := &config.Config{PaymentBackend: config.PaymentBackendHosted, PaymentGatewayURL: gw.URL, PaymentGatewayAPIKey: "pgk_test"}

	req := httptest.NewRequest(http.MethodPost, "/admin/billing/topup-checkout",
		bytes.NewBufferString(`{"amount_usd_cents":100,"success_url":"http://x/s","cancel_url":"http://x/c"}`))
	rec := httptest.NewRecorder()
	AdminBillingTopupCheckout(nil, cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (gateway status preserved)", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "internal") || strings.Contains(body, "secret") {
		t.Fatalf("raw gateway body leaked to the browser: %s", body)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["error"] != "amount below minimum" {
		t.Fatalf("want {error: amount below minimum}, got %s", body)
	}
}

// TestBillingRelay_SuccessPassesThrough keeps the success contract: a 2xx
// gateway answer is relayed byte-for-byte.
func TestBillingRelay_SuccessPassesThrough(t *testing.T) {
	const ok = `{"session_id":"cs_1","url":"https://checkout.test/cs_1","credit_atto":"2966"}`
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ok))
	}))
	defer gw.Close()
	cfg := &config.Config{PaymentBackend: config.PaymentBackendHosted, PaymentGatewayURL: gw.URL, PaymentGatewayAPIKey: "pgk_test"}

	req := httptest.NewRequest(http.MethodPost, "/admin/billing/topup-checkout",
		bytes.NewBufferString(`{"amount_usd_cents":100,"success_url":"http://x/s","cancel_url":"http://x/c"}`))
	rec := httptest.NewRecorder()
	AdminBillingTopupCheckout(nil, cfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != ok {
		t.Fatalf("success must pass through unmodified: %d %s", rec.Code, rec.Body.String())
	}
}

// TestGatewayPricingCache_KeyedByGateway proves two configs pointing at two
// gateways never serve each other's numbers, and each gateway is asked once
// per TTL (#163 review, V2-1269).
func TestGatewayPricingCache_KeyedByGateway(t *testing.T) {
	mk := func(rate string, calls *int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*calls++
			_ = json.NewEncoder(w).Encode(map[string]any{"balance": "0", "rate_usd_per_ant": rate})
		}))
	}
	var callsA, callsB int
	a := mk("0.11", &callsA)
	defer a.Close()
	b := mk("0.22", &callsB)
	defer b.Close()
	cfgA := &config.Config{PaymentGatewayURL: a.URL, PaymentGatewayAPIKey: "pgk_a"}
	cfgB := &config.Config{PaymentGatewayURL: b.URL, PaymentGatewayAPIKey: "pgk_b"}

	if got := cachedGatewayPricing(context.Background(), cfgA).rate; got != "0.11" {
		t.Fatalf("gateway A rate = %q, want 0.11", got)
	}
	if got := cachedGatewayPricing(context.Background(), cfgB).rate; got != "0.22" {
		t.Fatalf("gateway B rate = %q, want 0.22 (not A's cached value)", got)
	}
	if got := cachedGatewayPricing(context.Background(), cfgA).rate; got != "0.11" {
		t.Fatalf("gateway A second read = %q, want 0.11", got)
	}
	if callsA != 1 || callsB != 1 {
		t.Fatalf("within one TTL each gateway must be asked once: A=%d B=%d", callsA, callsB)
	}
}
