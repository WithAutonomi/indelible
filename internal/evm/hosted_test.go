package evm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		"status": "insufficient_credits",
		"error":  "insufficient credits: balance 0 < total 35156250000000000",
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
		"35156250000000000":     "0.03515625",
		"71358258928571428571":  "71.358258928571428571",
		"1000000000000000000":   "1",
		"0":                     "0",
		"":                      "",
		"not-a-number":          "not-a-number",
	} {
		if got := attoToANT(in); got != want {
			t.Errorf("attoToANT(%q) = %q, want %q", in, got, want)
		}
	}
}
