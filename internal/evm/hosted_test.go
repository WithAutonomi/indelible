package evm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

// stubGateway answers /pay and /account with canned bodies.
func stubGateway(t *testing.T, payStatus int, payBody map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pgk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/pay":
			w.WriteHeader(payStatus)
			_ = json.NewEncoder(w).Encode(payBody)
		case "/account":
			_ = json.NewEncoder(w).Encode(map[string]any{"account": "acme", "balance": "71358258928571428571"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestHostedPayForQuotesPaid(t *testing.T) {
	srv := stubGateway(t, http.StatusOK, map[string]any{
		"status": "paid", "pay_tx_hash": "0xabc",
		"tx_hashes":   map[string]string{"0xq1": "0xabc"},
		"payment_key": "deadbeef",
	})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	hashes, key, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err != nil || hashes["0xq1"] != "0xabc" || key != "deadbeef" {
		t.Fatalf("paid path: %v %v %q", hashes, err, key)
	}
}

func TestHostedInsufficientCreditsIsHumanReadable(t *testing.T) {
	srv := stubGateway(t, http.StatusPaymentRequired, map[string]any{
		"status":       "insufficient_credits",
		"error":        "insufficient credits: balance 0 < total 35156250000000000",
		"total_amount": "35156250000000000",
	})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	_, _, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "0.03515625 ANT") || !strings.Contains(msg, "top up") {
		t.Fatalf("message not operator-friendly: %q", msg)
	}
	if strings.Contains(msg, "35156250000000000") || strings.Contains(msg, "402") {
		t.Fatalf("raw atto/status leaked into the message: %q", msg)
	}
}

func TestHostedPaymentCostFromPaidResponse(t *testing.T) {
	srv := stubGateway(t, http.StatusOK, map[string]any{
		"status": "paid", "pay_tx_hash": "0xabc",
		"tx_hashes":     map[string]string{"0xq1": "0xabc"},
		"payment_key":   "deadbeef",
		"total_amount":  "35156250000000000",
		"fee_amount":    "5000000000000000",
		"total_debited": "40156250000000000",
	})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	if _, _, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv"); err != nil {
		t.Fatal(err)
	}
	c, ok := h.PaymentCost("deadbeef")
	if !ok || c.FeeAtto != "5000000000000000" || c.TotalDebited != "40156250000000000" {
		t.Fatalf("payment cost: %+v ok=%v", c, ok)
	}
	// Pop semantics: collected once, then gone.
	if _, ok := h.PaymentCost("deadbeef"); ok {
		t.Fatal("cost must be popped on read")
	}
}

func TestHostedAsyncPollCarriesFee(t *testing.T) {
	srv := asyncGateway(t, "paid", map[string]any{
		"pay_tx_hash": "0xdef", "tx_hashes": map[string]string{"0xq1": "0xdef"},
		"total_amount": "35156250000000000", "fee_amount": "5000000000000000",
		"total_debited": "40156250000000000"})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	h.SetPollWait(30 * time.Second)
	if _, _, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv"); err != nil {
		t.Fatal(err)
	}
	c, ok := h.PaymentCost("k123")
	if !ok || c.TotalDebited != "40156250000000000" {
		t.Fatalf("poll path lost the fee itemization: %+v ok=%v", c, ok)
	}
}

func TestHostedInsufficientCreditsNamesBothParts(t *testing.T) {
	// Fiat form: gross + itemized fee, still no atto/ANT leak.
	srv := stubGateway(t, http.StatusPaymentRequired, map[string]any{
		"status":          "insufficient_credits",
		"error":           "insufficient credits: balance 0 < total 35156250000000000 + 5000000000000000 network fee",
		"total_amount":    "35156250000000000",
		"fee_amount":      "5000000000000000",
		"total_debited":   "40156250000000000",
		"total_usd_cents": 2,
		"fee_usd_cents":   1,
	})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	_, _, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "$0.02") || !strings.Contains(msg, "$0.01 network fee") {
		t.Fatalf("fiat message does not name both parts: %q", msg)
	}
	if strings.Contains(msg, " ANT") || strings.Contains(msg, "atto") {
		t.Fatalf("crypto leaked into the fiat message: %q", msg)
	}

	// ANT fallback (no rate): both parts in ANT.
	srv2 := stubGateway(t, http.StatusPaymentRequired, map[string]any{
		"status":        "insufficient_credits",
		"total_amount":  "35156250000000000",
		"fee_amount":    "5000000000000000",
		"total_debited": "40156250000000000",
	})
	defer srv2.Close()
	h2 := NewHostedPayer(srv2.URL, "pgk_test")
	_, _, err = h2.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); !strings.Contains(msg, "0.03515625 ANT") || !strings.Contains(msg, "0.005 ANT network fee") {
		t.Fatalf("ANT fallback does not name both parts: %q", msg)
	}
}

func TestHostedAccountBalance(t *testing.T) {
	srv := stubGateway(t, http.StatusOK, nil)
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	bal, err := h.AccountBalance(context.Background())
	if err != nil || bal != "71358258928571428571" {
		t.Fatalf("balance: %q %v", bal, err)
	}
}

func TestAttoToANT(t *testing.T) {
	for in, want := range map[string]string{
		"35156250000000000":    "0.03515625",
		"71358258928571428571": "71.358258928571428571",
		"1000000000000000000":  "1",
		"0":                    "0",
		"":                     "",
		"not-a-number":         "not-a-number",
	} {
		if got := attoToANT(in); got != want {
			t.Errorf("attoToANT(%q) = %q, want %q", in, got, want)
		}
	}
}

// asyncGateway stubs the V2-925 contract: /pay answers 202 with a
// payment_key; /payments/{key} advances queued → paid across polls.
func asyncGateway(t *testing.T, terminal string, terminalBody map[string]any) *httptest.Server {
	t.Helper()
	polls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/pay":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "unconfirmed", "payment_key": "k123",
				"error": "payment queued for broadcast — poll GET /payments/k123"})
		case r.URL.Path == "/payments/k123":
			polls++
			if polls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "queued"})
				return
			}
			body := map[string]any{"status": terminal}
			for k, v := range terminalBody {
				body[k] = v
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestHostedAsyncPollToPaid(t *testing.T) {
	srv := asyncGateway(t, "paid", map[string]any{
		"pay_tx_hash": "0xdef", "tx_hashes": map[string]string{"0xq1": "0xdef"}})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	h.SetPollWait(30 * time.Second)
	hashes, key, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err != nil || hashes["0xq1"] != "0xdef" || key != "k123" {
		t.Fatalf("async paid: %v %q %v", hashes, key, err)
	}
}

func TestHostedAsyncPollToFailed(t *testing.T) {
	srv := asyncGateway(t, "failed", map[string]any{"error": "transaction reverted: 0xbad"})
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	h.SetPollWait(30 * time.Second)
	_, _, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err == nil || !strings.Contains(err.Error(), "reverted") {
		t.Fatalf("async failed path: %v", err)
	}
	if errors.Is(err, ErrConfirmationTimeout) {
		t.Fatal("a definitive failure must not map to the preserve path")
	}
}

// TestHostedBillingRelays proves the V2-1097 relay methods pass the
// gateway's status and body through unmodified — success and refusal alike
// — with the tenant key attached server-side.
func TestHostedBillingRelays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pgk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/topup/checkout":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["amount_usd_cents"].(float64) == 2500 &&
				req["success_url"] != "http://app.local/admin/billing?topup={CHECKOUT_SESSION_ID}" {
				t.Errorf("success_url not relayed: %v", req["success_url"])
			}
			if req["amount_usd_cents"].(float64) == 100 { // below gateway bounds
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "amount_usd_cents 100 outside bounds 500–1000000"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "cs_test_1", "url": "https://checkout.stripe.com/pay/cs_test_1", "credit_atto": "71428571428571428571"})
		case "/topup/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"credited": false, "payment_status": "unpaid"})
		case "/topups":
			_ = json.NewEncoder(w).Encode(map[string]any{"topups": []map[string]any{{"id": 1, "session_id": "cs_test_1"}}})
		case "/credits":
			_ = json.NewEncoder(w).Encode(map[string]any{"credits": []map[string]any{
				{"id": 2, "source": "card", "amount_usd_cents": 2500},
				{"id": 1, "source": "grant", "note": "invoice #77"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")

	status, raw, err := h.TopupCheckout(context.Background(), 2500,
		"http://app.local/admin/billing?topup={CHECKOUT_SESSION_ID}", "http://app.local/admin/billing?cancelled=1")
	if err != nil || status != http.StatusOK || !strings.Contains(string(raw), "cs_test_1") {
		t.Fatalf("checkout relay: status=%d err=%v body=%s", status, err, raw)
	}

	status, raw, err = h.TopupCheckout(context.Background(), 100, "http://app.local/x", "http://app.local/y")
	if err != nil || status != http.StatusBadRequest || !strings.Contains(string(raw), "outside bounds") {
		t.Fatalf("bounds refusal must pass through: status=%d err=%v body=%s", status, err, raw)
	}

	status, raw, err = h.TopupSync(context.Background(), "cs_test_1")
	if err != nil || status != http.StatusOK || !strings.Contains(string(raw), `"credited":false`) {
		t.Fatalf("sync relay: status=%d err=%v body=%s", status, err, raw)
	}

	status, raw, err = h.Topups(context.Background())
	if err != nil || status != http.StatusOK || !strings.Contains(string(raw), `"topups"`) {
		t.Fatalf("topups relay: status=%d err=%v body=%s", status, err, raw)
	}

	status, raw, err = h.Credits(context.Background())
	if err != nil || status != http.StatusOK || !strings.Contains(string(raw), `"source":"grant"`) {
		t.Fatalf("credits relay: status=%d err=%v body=%s", status, err, raw)
	}

	if _, _, err := NewHostedPayer("http://127.0.0.1:1", "pgk_test").Topups(context.Background()); err == nil {
		t.Fatal("transport failure must surface as an error")
	}
}

// TestHostedFiatSurface proves the V2-1100 crypto-free plumbing: the 402
// message speaks fiat when the gateway supplies cents (ANT only as the
// fallback), and AccountInfo carries the rate.
func TestHostedFiatSurface(t *testing.T) {
	cents := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pay":
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "insufficient_credits", "total_amount": "35156250000000000",
				"total_usd_cents": cents, "tx_hashes": map[string]string{}})
		case "/account":
			_ = json.NewEncoder(w).Encode(map[string]any{"account": "acme", "balance": "5", "rate_usd_per_ant": "0.35"})
		}
	}))
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")

	cents = 2
	_, _, err := h.PayForQuotes(context.Background(), "", nil, nil, "0xt", "0xv")
	if err == nil || !strings.Contains(err.Error(), "about $0.02 of storage credit") {
		t.Fatalf("fiat 402 message wrong: %v", err)
	}
	if strings.Contains(err.Error(), "ANT") {
		t.Fatalf("fiat message must not mention ANT: %v", err)
	}

	cents = 0 // no rate configured gateway-side → ANT fallback
	_, _, err = h.PayForQuotes(context.Background(), "", nil, nil, "0xt", "0xv")
	if err == nil || !strings.Contains(err.Error(), "0.03515625 ANT") {
		t.Fatalf("ANT fallback message wrong: %v", err)
	}

	bal, rate, fee, err := h.AccountInfo(context.Background())
	if err != nil || bal != "5" || rate != "0.35" {
		t.Fatalf("AccountInfo: %q %q %v", bal, rate, err)
	}
	// Older gateway without fee_per_batch_atto: tolerated as empty, no error.
	if fee != "" {
		t.Fatalf("absent fee must relay as empty, got %q", fee)
	}
}

// TestHostedFeePerBatchRelay proves the V2-1113 fee relay: AccountInfo
// carries fee_per_batch_atto and FeePerBatch turns it into an exact big.Int
// — present, absent (older gateway), and zero all tolerated without error.
func TestHostedFeePerBatchRelay(t *testing.T) {
	for name, tc := range map[string]struct {
		account map[string]any
		want    string
	}{
		"fee present": {map[string]any{"account": "acme", "balance": "5",
			"rate_usd_per_ant": "0.35", "fee_per_batch_atto": "5000000000000000"}, "5000000000000000"},
		"fee absent (older gateway)": {map[string]any{"account": "acme", "balance": "5",
			"rate_usd_per_ant": "0.35"}, "0"},
		"fee zero": {map[string]any{"account": "acme", "balance": "5",
			"fee_per_batch_atto": "0"}, "0"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tc.account)
			}))
			defer srv.Close()
			h := NewHostedPayer(srv.URL, "pgk_test")
			if got := h.FeePerBatch(context.Background()); got.String() != tc.want {
				t.Errorf("FeePerBatch = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestHostedFeePerBatchCache proves the fee lookup's cache discipline: one
// gateway call per TTL window, a mid-window fee change invisible until the
// window rolls, and an unreachable gateway serving the last known value
// rather than erroring or zeroing (a stale fee beats a wrongly-net ceiling).
func TestHostedFeePerBatchCache(t *testing.T) {
	calls := 0
	fee := "5000000000000000"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"balance": "5", "fee_per_batch_atto": fee})
	}))
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")

	if got := h.FeePerBatch(context.Background()); got.String() != "5000000000000000" {
		t.Fatalf("first fetch: %s", got)
	}
	fee = "9000000000000000"
	if got := h.FeePerBatch(context.Background()); got.String() != "5000000000000000" {
		t.Fatalf("within TTL the cached fee must serve, got %s", got)
	}
	if calls != 1 {
		t.Fatalf("gateway asked %d times within one TTL, want 1", calls)
	}

	h.feeTTL = 0 // expire the window
	if got := h.FeePerBatch(context.Background()); got.String() != "9000000000000000" {
		t.Fatalf("expired window must refetch, got %s", got)
	}

	// Gateway gone: the last known value keeps serving.
	srv.Close()
	if got := h.FeePerBatch(context.Background()); got.String() != "9000000000000000" {
		t.Fatalf("unreachable gateway must serve last known fee, got %s", got)
	}

	// Never fetched successfully at all → zero, still no error path.
	dead := NewHostedPayer("http://127.0.0.1:1", "pgk_test")
	if got := dead.FeePerBatch(context.Background()); got.Sign() != 0 {
		t.Fatalf("unknown fee must count as zero, got %s", got)
	}
}

// TestHostedPayTransportRetry proves the /pay resend (V2-931 case I root
// cause): the gateway dying mid-request EOFs the connection; the batch is
// idempotent gateway-side, so the payer resends and the upload survives the
// crash window instead of failing on a payment that actually went through.
func TestHostedPayTransportRetry(t *testing.T) {
	drops := 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if drops > 0 {
			drops--
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("no hijacker")
			}
			conn, _, _ := hj.Hijack()
			_ = conn.Close() // client sees EOF — a killed gateway
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "paid", "pay_tx_hash": "0xabc",
			"tx_hashes": map[string]string{"0xq1": "0xabc"}, "payment_key": "k9"})
	}))
	defer srv.Close()
	h := NewHostedPayer(srv.URL, "pgk_test")
	h.payRetryWait = 10 * time.Millisecond
	hashes, key, err := h.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err != nil || hashes["0xq1"] != "0xabc" || key != "k9" {
		t.Fatalf("retry path: %v %v %q", hashes, err, key)
	}

	// Exhaustion still surfaces as unreachable.
	dead := NewHostedPayer("http://127.0.0.1:1", "pgk_test")
	dead.payAttempts, dead.payRetryWait = 2, time.Millisecond
	_, _, err = dead.PayForQuotes(context.Background(), "", []antd.PaymentInfo{{QuoteHash: "0xq1"}}, nil, "0xt", "0xv")
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("exhaustion must report unreachable: %v", err)
	}
}
