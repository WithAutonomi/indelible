package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

// HostedPayer settles payments through a payment gateway's POST /pay instead
// of signing locally (payment_backend=hosted, V2-929 PoC). It satisfies the same
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
	// payAttempts/payRetryWait bound the transport-level resend of POST
	// /pay (safe: the batch is idempotent gateway-side). Sized to outlast
	// a gateway restart.
	payAttempts  int
	payRetryWait time.Duration
	// costs holds each paid batch's fee itemization keyed by payment_key
	// until the worker collects it via PaymentCost (V2-1098) — the payer
	// interface returns only the tx map, and one HostedPayer serves
	// concurrent uploads, so a "last payment" field would race.
	costsMu sync.Mutex
	costs   map[string]PaymentCost
	// fee* memoize the gateway's per-batch network fee for the pre-spend
	// gross estimate/ceiling (V2-1113) — the worker asks per upload, the
	// gateway at most once per feeTTL (the wallet-status rate cadence,
	// V2-1100). feeCached nil means never fetched successfully.
	feeMu      sync.Mutex
	feeCached  *big.Int
	feeFetched time.Time
	feeTTL     time.Duration
}

// PaymentCost is a paid batch's fee itemization: what the gateway debited
// beyond the batch total (V2-1098).
type PaymentCost struct {
	FeeAtto      string // the network fee, atto
	TotalDebited string // batch total + fee — the gross debit
}

// NewHostedPayer builds a payer that delegates to the gateway at gatewayURL,
// authenticating as this instance's tenant account via apiKey (Bearer).
func NewHostedPayer(gatewayURL, apiKey string) *HostedPayer {
	return &HostedPayer{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		apiKey:     apiKey,
		// No overall timeout: /pay legitimately blocks for the gateway's
		// sync wait, mirroring the local signer's bound.
		client:       &http.Client{},
		pollWait:     5 * time.Minute,
		payAttempts:  6,
		payRetryWait: 5 * time.Second,
		feeTTL:       time.Minute,
	}
}

// SetPollWait overrides the async-completion polling bound.
func (h *HostedPayer) SetPollWait(d time.Duration) { h.pollWait = d }

// rememberCost stashes a paid response's fee itemization for the worker.
func (h *HostedPayer) rememberCost(paymentKey string, resp *hostedPayResponse) {
	if paymentKey == "" || resp.FeeAmount == "" || resp.TotalDebited == "" {
		return
	}
	h.costsMu.Lock()
	defer h.costsMu.Unlock()
	if h.costs == nil {
		h.costs = map[string]PaymentCost{}
	}
	h.costs[paymentKey] = PaymentCost{FeeAtto: resp.FeeAmount, TotalDebited: resp.TotalDebited}
}

// PaymentCost pops the fee itemization for a payment key, if the gateway
// reported one — the worker records the GROSS spend on the upload and its
// transaction so the customer's books match the gateway ledger (V2-1098).
func (h *HostedPayer) PaymentCost(paymentKey string) (PaymentCost, bool) {
	h.costsMu.Lock()
	defer h.costsMu.Unlock()
	c, ok := h.costs[paymentKey]
	if ok {
		delete(h.costs, paymentKey)
	}
	return c, ok
}

// hostedPayRequest is the /pay body. The tenant is never named here: the
// gateway resolves it from the Bearer API key and treats any account_id in
// the body as advisory at most, so sending one only invites confusion.
type hostedPayRequest struct {
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
	// TotalUSDCents is the GROSS debit (batch total + network fee) in fiat
	// at the gateway's rate (rounded up), when the gateway has a rate
	// configured — the crypto-free number user-facing messages prefer
	// (V2-1100); gross so "top up $X" always suffices (V2-1098).
	TotalUSDCents int64 `json:"total_usd_cents"`
	// FeeAmount/TotalDebited itemize the gateway's per-batch network fee
	// (V2-1098): fee in atto and total_amount + fee — what the account was
	// actually debited. Empty when the gateway charges no fee.
	FeeAmount    string `json:"fee_amount"`
	TotalDebited string `json:"total_debited"`
	// FeeUSDCents is the fee alone in fiat (rounded up), rate permitting.
	FeeUSDCents int64  `json:"fee_usd_cents"`
	Error       string `json:"error"`
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
		PaymentType:         "wave_batch",
		Payments:            payments,
		TokenAddress:        tokenAddress,
		PaymentVaultAddress: dataPaymentsAddress,
		SignedQuotes:        signedQuotes,
	})
	if err != nil {
		return nil, "", fmt.Errorf("encoding /pay request: %w", err)
	}

	// Transport failures are retried: the batch is content-addressed
	// idempotent gateway-side (one payments record per idempotency key,
	// ever — V2-924), so resending can never double-pay. This covers the
	// gateway dying with our request in flight — the killed connection
	// EOFs, the gateway restarts, and the resend lands on the idempotent
	// replay path. Without it a crash in the narrow pre-202 window fails
	// the upload even though the payment itself survives (V2-931 case I).
	var resp *http.Response
	for attempt := 1; ; attempt++ {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, h.gatewayURL+"/pay", bytes.NewReader(body))
		if rerr != nil {
			return nil, "", rerr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
		resp, err = h.client.Do(req)
		if err == nil {
			break
		}
		if attempt >= h.payAttempts {
			return nil, "", fmt.Errorf("payment gateway unreachable: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(h.payRetryWait):
		}
	}
	defer resp.Body.Close()

	var payResp hostedPayResponse
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payResp); err != nil {
		return nil, "", fmt.Errorf("payment gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	switch {
	case resp.StatusCode == http.StatusOK && (payResp.Status == "paid" || payResp.Status == "nothing_to_pay"):
		h.rememberCost(payResp.PaymentKey, &payResp)
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
		// and what to do (V2-930) — in fiat when the gateway has a rate
		// (crypto-free counter, V2-1100), ANT only as the fallback. Both
		// parts named when the gateway charges a network fee (V2-1098).
		if payResp.TotalUSDCents > 0 {
			if payResp.FeeUSDCents > 0 {
				return nil, "", fmt.Errorf(
					"insufficient gateway credits: this upload needs about $%d.%02d of storage credit (including a $%d.%02d network fee) — top up and retry (retrying is safe, nothing was paid)",
					payResp.TotalUSDCents/100, payResp.TotalUSDCents%100,
					payResp.FeeUSDCents/100, payResp.FeeUSDCents%100)
			}
			return nil, "", fmt.Errorf(
				"insufficient gateway credits: this upload needs about $%d.%02d of storage credit — top up and retry (retrying is safe, nothing was paid)",
				payResp.TotalUSDCents/100, payResp.TotalUSDCents%100)
		}
		if payResp.FeeAmount != "" {
			return nil, "", fmt.Errorf(
				"insufficient gateway credits: this upload needs %s ANT + %s ANT network fee — top up the account's credits and retry (retrying is safe, nothing was paid)",
				attoToANT(payResp.TotalAmount), attoToANT(payResp.FeeAmount))
		}
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
					h.rememberCost(paymentKey, &payResp)
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

// AccountDetails is the gateway's GET /account answer as the display
// surfaces relay it: balance, the exact USD-per-ANT rate (V2-1100), the
// per-batch network fee (V2-1113), and the directional cost-per-GB estimate
// with its methodology basis (V2-1114). Every field beyond the balance is
// optional — empty/nil when the gateway has none configured, has too little
// paid history yet, or simply predates the field. Absence is never an error.
type AccountDetails struct {
	BalanceAtto     string
	RateUSDPerANT   string
	FeePerBatchAtto string
	// EstCostPerGBAtto is what ≈1 GB of fresh data costs at current prices,
	// estimated by the gateway from its own recent paid history; "" = no
	// estimate ("not enough data yet" client-side, never zero).
	EstCostPerGBAtto string
	// EstCostPerGBBasis is the estimate's basis object (median paid per
	// quote, sample size, window, chunk/batch constants), relayed opaquely so
	// the client tooltip hardcodes no methodology numbers.
	EstCostPerGBBasis json.RawMessage
}

// AccountDetails fetches GET /account once and returns everything the
// billing surfaces relay (see the AccountDetails type).
func (h *HostedPayer) AccountDetails(ctx context.Context) (AccountDetails, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.gatewayURL+"/account", nil)
	if err != nil {
		return AccountDetails{}, err
	}
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return AccountDetails{}, fmt.Errorf("payment gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Balance   string          `json:"balance"`
		Rate      string          `json:"rate_usd_per_ant"`
		Fee       string          `json:"fee_per_batch_atto"`
		EstCostGB string          `json:"est_cost_per_gb_atto"`
		EstBasis  json.RawMessage `json:"est_cost_per_gb_basis"`
		Error     string          `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return AccountDetails{}, fmt.Errorf("decoding /account response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return AccountDetails{}, fmt.Errorf("payment gateway /account failed (%d): %s", resp.StatusCode, out.Error)
	}
	return AccountDetails{
		BalanceAtto:       out.Balance,
		RateUSDPerANT:     out.Rate,
		FeePerBatchAtto:   out.Fee,
		EstCostPerGBAtto:  out.EstCostGB,
		EstCostPerGBBasis: out.EstBasis,
	}, nil
}

// AccountInfo returns the tenant's remaining gateway credit balance in atto
// plus the gateway's exact USD-per-ANT rate and per-batch network fee in
// atto (each empty when none configured, or when an older gateway predates
// the field) — the trio the crypto-free display converts with (V2-1100) and
// fee-aware estimates add with (V2-1113). Thin wrapper over AccountDetails
// for the callers that need no more.
func (h *HostedPayer) AccountInfo(ctx context.Context) (balance, rateUSDPerANT, feePerBatchAtto string, err error) {
	d, err := h.AccountDetails(ctx)
	return d.BalanceAtto, d.RateUSDPerANT, d.FeePerBatchAtto, err
}

// AccountBalance returns the tenant's remaining gateway credit balance in
// atto (GET /account) — the meaningful "balance after" for hosted payments,
// where neither the wallet record nor the treasury is the payer's account.
func (h *HostedPayer) AccountBalance(ctx context.Context) (string, error) {
	bal, _, _, err := h.AccountInfo(ctx)
	return bal, err
}

// FeePerBatch returns the gateway's per-batch network fee in atto — the
// V2-1098 surcharge every settled batch adds to the debit — so pre-spend
// surfaces can quote and gate on the same GROSS basis the gateway actually
// charges (V2-1113). Cached for feeTTL; while the gateway is unreachable the
// last known value keeps serving (fee changes are rare, refusing uploads over
// a stale fee lookup would be worse), and a gateway that reports no fee —
// none configured, or an older gateway without the field — counts as zero.
//
// The bool is false only when NO fetch has ever succeeded: then the fee is
// not "zero", it is unknown, and a caller gating spend on the gross cost must
// not treat it as zero (review of #163). Once a fetch has succeeded the value
// is known, even if stale.
func (h *HostedPayer) FeePerBatch(ctx context.Context) (*big.Int, bool) {
	h.feeMu.Lock()
	defer h.feeMu.Unlock()
	if h.feeFetched.IsZero() || time.Since(h.feeFetched) >= h.feeTTL {
		feeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, _, fee, err := h.AccountInfo(feeCtx)
		cancel()
		if err == nil {
			if v, ok := new(big.Int).SetString(strings.TrimSpace(fee), 10); ok && v.Sign() > 0 {
				h.feeCached = v
			} else {
				h.feeCached = new(big.Int) // absent/zero/junk → no fee
			}
		}
		// Set even on error: retry after the normal interval, not per call.
		h.feeFetched = time.Now()
	}
	if h.feeCached == nil {
		return new(big.Int), false
	}
	return new(big.Int).Set(h.feeCached), true
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

// Credits relays the tenant's full credit history (GET /credits) — card
// top-ups and invoice-path grants alike, so the Billing screen answers
// "where did this credit come from" for every funding path.
func (h *HostedPayer) Credits(ctx context.Context) (int, []byte, error) {
	return h.relay(ctx, http.MethodGet, "/credits", nil)
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
