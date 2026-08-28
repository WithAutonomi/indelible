package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

// HostedPayer settles payments through a payment gateway's POST /pay instead
// of signing locally (payment_mode=hosted, V2-929 PoC). It satisfies the same
// method set as Signer so the upload worker can hold either behind one
// interface. The privateKeyHex arguments are ignored — the gateway holds the
// treasury key.
type HostedPayer struct {
	gatewayURL string
	apiKey     string
	client     *http.Client
	// pollWait bounds how long a 202-with-payment_key answer is polled via
	// GET /payments/{key} before falling back to the preserve path.
	pollWait time.Duration
}

// NewHostedPayer builds a payer that delegates to the gateway at gatewayURL,
// authenticating as this instance's tenant account via apiKey (Bearer).
func NewHostedPayer(gatewayURL, apiKey string) *HostedPayer {
	return &HostedPayer{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		apiKey:     apiKey,
		// No overall timeout: /pay legitimately blocks for the gateway's
		// sync wait, mirroring the local signer's bound.
		client:   &http.Client{},
		pollWait: 5 * time.Minute,
	}
}

// SetPollWait overrides the async-completion polling bound.
func (h *HostedPayer) SetPollWait(d time.Duration) { h.pollWait = d }

type hostedPayRequest struct {
	AccountID           string                  `json:"account_id"`
	PaymentType         string                  `json:"payment_type,omitempty"`
	Payments            []antd.PaymentInfo      `json:"payments"`
	TokenAddress        string                  `json:"token_address"`
	PaymentVaultAddress string                  `json:"payment_vault_address"`
	SignedQuotes        []antd.SignedQuoteEntry `json:"signed_quotes,omitempty"`
}

type hostedPayResponse struct {
	Status      string            `json:"status"`
	Replayed    bool              `json:"replayed"`
	PayTxHash   string            `json:"pay_tx_hash"`
	TxHashes    map[string]string `json:"tx_hashes"`
	TotalAmount string            `json:"total_amount"`
	Error       string            `json:"error"`
	// PaymentKey is the gateway's batch idempotency key — persisted on the
	// upload as its provenance join to the gateway ledger (V2-1086).
	PaymentKey string `json:"payment_key"`
}

// PayForQuotes submits the batch to the gateway and returns the
// quote_hash → tx_hash map antd's finalize expects. The signed quotes are
// relayed unmodified so the gateway can verify the batch offline before
// paying (V2-926). A gateway "unconfirmed" answer (202) is surfaced as
// ErrConfirmationTimeout so the worker preserves the upload for
// reconciliation instead of re-paying; a "rejected" answer is a permanent
// refusal — the worker abandons rather than retrying.
func (h *HostedPayer) PayForQuotes(
	ctx context.Context,
	_ string, // private key unused — the gateway signs
	payments []antd.PaymentInfo,
	signedQuotes []antd.SignedQuoteEntry,
	tokenAddress string,
	dataPaymentsAddress string,
) (map[string]string, string, error) {
	body, err := json.Marshal(hostedPayRequest{
		AccountID:           "indelible-poc",
		PaymentType:         "wave_batch",
		Payments:            payments,
		TokenAddress:        tokenAddress,
		PaymentVaultAddress: dataPaymentsAddress,
		SignedQuotes:        signedQuotes,
	})
	if err != nil {
		return nil, "", fmt.Errorf("encoding /pay request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.gatewayURL+"/pay", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("payment gateway unreachable: %w", err)
	}
	defer resp.Body.Close()

	var payResp hostedPayResponse
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payResp); err != nil {
		return nil, "", fmt.Errorf("payment gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	switch {
	case resp.StatusCode == http.StatusOK && (payResp.Status == "paid" || payResp.Status == "nothing_to_pay"):
		return payResp.TxHashes, payResp.PaymentKey, nil
	case resp.StatusCode == http.StatusAccepted && payResp.Status == "unconfirmed":
		// Async contract (V-925/929): the gateway queued or broadcast the
		// batch and handed back its payment_key — poll to completion. Only
		// when polling exhausts (or no key was given) fall back to the
		// preserve path via the same typed error as a local confirmation
		// timeout, so classifyFailure never re-pays.
		if payResp.PaymentKey != "" {
			return h.pollPayment(ctx, payResp.PaymentKey, payResp.PayTxHash)
		}
		return nil, "", fmt.Errorf("%w (gateway tx %s)", ErrConfirmationTimeout, payResp.PayTxHash)
	case payResp.Status == "insufficient_credits":
		// The one refusal an operator fixes themselves: say what it costs
		// and what to do, in ANT, not a wrapped error chain (V2-930).
		return nil, "", fmt.Errorf(
			"insufficient gateway credits: this upload needs %s ANT — top up the account's credits and retry (retrying is safe, nothing was paid)",
			attoToANT(payResp.TotalAmount))
	default:
		msg := payResp.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, "", fmt.Errorf("payment gateway /pay failed (%d, %s): %s", resp.StatusCode, payResp.Status, msg)
	}
}

// attoToANT renders an atto amount (decimal string) as a human ANT figure —
// integer string math, never floats. Unparseable input passes through.
func attoToANT(atto string) string {
	s := strings.TrimSpace(atto)
	if s == "" || strings.ContainsAny(s, ".-") {
		return atto
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return atto
		}
	}
	if len(s) <= 18 {
		s = strings.Repeat("0", 19-len(s)) + s
	}
	whole, frac := s[:len(s)-18], strings.TrimRight(s[len(s)-18:], "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

// pollPayment follows an async payment to its terminal state via
// GET /payments/{key}. Transport errors keep polling (the payment is safe
// server-side; the gateway may be restarting); the bound falls back to the
// preserve path, never a re-pay.
func (h *HostedPayer) pollPayment(ctx context.Context, paymentKey, lastTx string) (map[string]string, string, error) {
	deadline := time.Now().Add(h.pollWait)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.gatewayURL+"/payments/"+paymentKey, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
		resp, err := h.client.Do(req)
		if err == nil {
			var payResp hostedPayResponse
			decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payResp)
			_ = resp.Body.Close()
			if decodeErr == nil && resp.StatusCode == http.StatusOK {
				if payResp.PayTxHash != "" {
					lastTx = payResp.PayTxHash
				}
				switch payResp.Status {
				case "paid":
					return payResp.TxHashes, paymentKey, nil
				case "failed", "rejected":
					return nil, "", fmt.Errorf("payment gateway reported %s: %s", payResp.Status, payResp.Error)
				}
			}
		}
		if time.Now().After(deadline) {
			return nil, "", fmt.Errorf("%w (gateway payment %s, tx %s)", ErrConfirmationTimeout, paymentKey, lastTx)
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// AccountBalance returns the tenant's remaining gateway credit balance in
// atto (GET /account) — the meaningful "balance after" for hosted payments,
// where neither the wallet record nor the treasury is the payer's account.
func (h *HostedPayer) AccountBalance(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.gatewayURL+"/account", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("payment gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Balance string `json:"balance"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding /account response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("payment gateway /account failed (%d): %s", resp.StatusCode, out.Error)
	}
	return out.Balance, nil
}

// relay performs one authenticated gateway call for the in-app billing
// surface (V2-1097) and hands back the gateway's status code + raw JSON so
// the caller can pass both through unmodified. Only transport-level failure
// is an error.
func (h *HostedPayer) relay(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(enc)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.gatewayURL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("payment gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// TopupCheckout relays a Stripe Checkout creation to the gateway
// (POST /topup/checkout): the gateway enforces bounds and validates the
// return URLs; the tenant API key never leaves the server.
func (h *HostedPayer) TopupCheckout(ctx context.Context, amountUSDCents int64, successURL, cancelURL string) (int, []byte, error) {
	return h.relay(ctx, http.MethodPost, "/topup/checkout", map[string]any{
		"amount_usd_cents": amountUSDCents,
		"success_url":      successURL,
		"cancel_url":       cancelURL,
	})
}

// TopupSync relays the deterministic credit fallback (POST /topup/sync) —
// used on return from Checkout so credits show without waiting on webhook
// delivery. Idempotent gateway-side.
func (h *HostedPayer) TopupSync(ctx context.Context, sessionID string) (int, []byte, error) {
	return h.relay(ctx, http.MethodPost, "/topup/sync", map[string]any{"session_id": sessionID})
}

// Topups relays the tenant's credited top-up history (GET /topups).
func (h *HostedPayer) Topups(ctx context.Context) (int, []byte, error) {
	return h.relay(ctx, http.MethodGet, "/topups", nil)
}

// PayForMerkleTree is not supported by the gateway PoC (merkle hosted support
// is V2-934).
func (h *HostedPayer) PayForMerkleTree(
	_ context.Context,
	_ string,
	_ int,
	_ []antd.PoolCommitmentEntry,
	_ uint64,
	_ string,
	_ string,
) (string, string, error) {
	return "", "", fmt.Errorf("hosted payment mode does not support merkle payments yet (V2-934)")
}

// GetBalances reports the gateway treasury's balances — in hosted mode the
// treasury is the paying wallet, so those are the balances worth recording.
func (h *HostedPayer) GetBalances(ctx context.Context, _ string, tokenAddress string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.gatewayURL+"/treasury?token="+tokenAddress, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("payment gateway unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		TokenBalance string `json:"token_balance"`
		GasBalance   string `json:"gas_balance"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("decoding /treasury response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("payment gateway /treasury failed (%d): %s", resp.StatusCode, out.Error)
	}
	return out.TokenBalance, out.GasBalance, nil
}

// SetConfirmationTimeout is a no-op: the confirmation bound lives on the
// gateway's signer in hosted mode.
func (h *HostedPayer) SetConfirmationTimeout(time.Duration) {}

// RPCUrl reports the gateway endpoint; the worker only uses it for its
// rebuild-on-change check and logging.
func (h *HostedPayer) RPCUrl() string {
	return h.gatewayURL
}
