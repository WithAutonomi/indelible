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
}

// NewHostedPayer builds a payer that delegates to the gateway at gatewayURL,
// authenticating as this instance's tenant account via apiKey (Bearer).
func NewHostedPayer(gatewayURL, apiKey string) *HostedPayer {
	return &HostedPayer{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		apiKey:     apiKey,
		// No overall timeout: /pay legitimately blocks for the gateway's
		// on-chain confirmation wait, mirroring the local signer's bound.
		client: &http.Client{},
	}
}

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
) (map[string]string, error) {
	body, err := json.Marshal(hostedPayRequest{
		AccountID:           "indelible-poc",
		PaymentType:         "wave_batch",
		Payments:            payments,
		TokenAddress:        tokenAddress,
		PaymentVaultAddress: dataPaymentsAddress,
		SignedQuotes:        signedQuotes,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding /pay request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.gatewayURL+"/pay", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("payment gateway unreachable: %w", err)
	}
	defer resp.Body.Close()

	var payResp hostedPayResponse
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payResp); err != nil {
		return nil, fmt.Errorf("payment gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	switch {
	case resp.StatusCode == http.StatusOK && (payResp.Status == "paid" || payResp.Status == "nothing_to_pay"):
		return payResp.TxHashes, nil
	case resp.StatusCode == http.StatusAccepted && payResp.Status == "unconfirmed":
		// Broadcast but unconfirmed on the gateway side: must map onto the
		// same typed error as a local confirmation timeout so classifyFailure
		// preserves the upload rather than re-paying.
		return nil, fmt.Errorf("%w (gateway tx %s)", ErrConfirmationTimeout, payResp.PayTxHash)
	default:
		msg := payResp.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("payment gateway /pay failed (%d, %s): %s", resp.StatusCode, payResp.Status, msg)
	}
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
