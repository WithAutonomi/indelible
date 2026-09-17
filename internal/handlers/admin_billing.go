package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/evm"
)

// Admin billing surface (V2-1097): the in-app home for hosted-mode funds.
// Every endpoint is a thin server-side relay to the payment gateway using
// the instance's tenant API key — the key must never reach the browser.
// The SPA supplies its own absolute return URLs (it knows its origin); the
// gateway validates them and Stripe lands the customer back on /admin/billing.

// billingPayer builds the gateway client, or writes the refusal and returns
// nil when the instance is not in hosted mode (these endpoints have no
// meaning for local signing).
func billingPayer(w http.ResponseWriter, cfg *config.Config) *evm.HostedPayer {
	if !cfg.PaymentBackend.Hosted() || cfg.PaymentGatewayURL == "" {
		jsonError(w, "billing is only available with the hosted payment backend", http.StatusBadRequest)
		return nil
	}
	return evm.NewHostedPayer(cfg.PaymentGatewayURL, cfg.PaymentGatewayAPIKey)
}

// relayOut passes a gateway SUCCESS answer through unmodified — status code
// and body both — so a successful checkout or sync reads identically whether
// the caller hit the gateway directly or via this relay.
//
// A non-2xx answer is NOT echoed: the browser gets the gateway's status code
// and only its short operator-facing `error` string (or a generic message),
// while the raw body is logged server-side. Gateway internals stay off the
// wire (#163 review, V2-1269).
func relayOut(w http.ResponseWriter, op string, status int, raw []byte) {
	if status >= 200 && status < 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(raw)
		return
	}
	logged := raw
	if len(logged) > 1024 {
		logged = logged[:1024]
	}
	slog.Warn("billing relay: gateway refused", "op", op, "status", status, "gateway_body", string(logged))
	jsonError(w, gatewayErrorMessage(status, raw), status)
}

// gatewayErrorMessage extracts the gateway's short `error` string from a
// refusal body — one line, bounded — or falls back to a generic message.
func gatewayErrorMessage(status int, raw []byte) string {
	var g struct {
		Error string `json:"error"`
	}
	msg := ""
	if json.Unmarshal(raw, &g) == nil {
		msg = strings.TrimSpace(g.Error)
	}
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if msg == "" {
		return "payment gateway refused the request (HTTP " + http.StatusText(status) + ")"
	}
	return msg
}

// @Summary      Billing summary
// @Description  Hosted-mode billing: gateway credits and credited top-up history
// @Tags         Admin: Billing
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string "Not in hosted payment mode"
// @Router       /admin/billing [get]
// @Security     BearerAuth
func AdminBillingSummary(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payer := billingPayer(w, cfg)
		if payer == nil {
			return
		}
		out := map[string]any{
			"payment_backend":     string(config.PaymentBackendHosted),
			"payment_gateway_url": cfg.PaymentGatewayURL,
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		// Credits and history are best-effort separately: a gateway hiccup
		// on one must not blank the other.
		if d, err := payer.AccountDetails(ctx); err == nil {
			out["gateway_credit_atto"] = d.BalanceAtto
			// Exact USD-per-ANT rate for fiat display (V2-1100).
			if d.RateUSDPerANT != "" {
				out["rate_usd_per_ant"] = d.RateUSDPerANT
			}
			// Per-batch network fee (V2-1098), relayed for fee-aware
			// estimates and billing transparency (V2-1113).
			if d.FeePerBatchAtto != "" {
				out["fee_per_batch_atto"] = d.FeePerBatchAtto
			}
			// Directional cost-per-GB estimate + methodology basis (V2-1114):
			// the "≈ N GB remaining at current prices" line beside the
			// balance. Rides the live balance call this summary already makes
			// (fresher than the wallet-status 60s cache, zero extra requests).
			// Absent — older gateway or thin paid history — hides the line.
			if d.EstCostPerGBAtto != "" {
				out["est_cost_per_gb_atto"] = d.EstCostPerGBAtto
				if len(d.EstCostPerGBBasis) > 0 {
					out["est_cost_per_gb_basis"] = d.EstCostPerGBBasis
				}
			}
		}
		if status, raw, err := payer.Credits(ctx); err == nil && status == http.StatusOK {
			var c struct {
				Credits json.RawMessage `json:"credits"`
			}
			if json.Unmarshal(raw, &c) == nil && c.Credits != nil {
				out["credits"] = c.Credits
			}
		}
		jsonResponse(w, http.StatusOK, out)
	}
}

type topupCheckoutRequest struct {
	AmountUSDCents int64 `json:"amount_usd_cents"`
	// Absolute URLs back into this instance's UI; validated by the gateway
	// (http/https only). Stripe substitutes {CHECKOUT_SESSION_ID} in
	// success_url if the placeholder is present.
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

// @Summary      Start a card top-up
// @Description  Create a Stripe Checkout session at the gateway; returns the hosted payment page URL and the exact credit
// @Tags         Admin: Billing
// @Accept       json
// @Produce      json
// @Param        body body topupCheckoutRequest true "Amount in USD cents and return URLs"
// @Success      200 {object} map[string]interface{} "session_id, url, credit_atto"
// @Failure      400 {object} map[string]string
// @Failure      502 {object} map[string]string "Gateway unreachable"
// @Router       /admin/billing/topup-checkout [post]
// @Security     BearerAuth
func AdminBillingTopupCheckout(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payer := billingPayer(w, cfg)
		if payer == nil {
			return
		}
		var req topupCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		status, raw, err := payer.TopupCheckout(r.Context(), req.AmountUSDCents, req.SuccessURL, req.CancelURL)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadGateway)
			return
		}
		relayOut(w, "topup-checkout", status, raw)
	}
}

type topupSyncRequest struct {
	SessionID string `json:"session_id"`
}

// @Summary      Sync a top-up
// @Description  Ask the gateway to retrieve the Checkout session from Stripe and credit it if paid (idempotent; webhook-loss fallback)
// @Tags         Admin: Billing
// @Accept       json
// @Produce      json
// @Param        body body topupSyncRequest true "Checkout session id"
// @Success      200 {object} map[string]interface{} "credited, payment_status"
// @Failure      400 {object} map[string]string
// @Failure      502 {object} map[string]string "Gateway unreachable"
// @Router       /admin/billing/topup-sync [post]
// @Security     BearerAuth
func AdminBillingTopupSync(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payer := billingPayer(w, cfg)
		if payer == nil {
			return
		}
		var req topupSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
			jsonError(w, "session_id required", http.StatusBadRequest)
			return
		}
		status, raw, err := payer.TopupSync(r.Context(), req.SessionID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadGateway)
			return
		}
		relayOut(w, "topup-sync", status, raw)
	}
}
