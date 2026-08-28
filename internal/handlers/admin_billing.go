package handlers

import (
	"context"
	"encoding/json"
	"net/http"
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
	if cfg.PaymentMode != "hosted" || cfg.PaymentGatewayURL == "" {
		jsonError(w, "billing is only available in hosted payment mode", http.StatusBadRequest)
		return nil
	}
	return evm.NewHostedPayer(cfg.PaymentGatewayURL, cfg.PaymentGatewayAPIKey)
}

// relayOut passes a gateway answer through unmodified — status code and body
// both — so bounds errors, refusals and successes read identically whether
// the caller hit the gateway directly or via this relay.
func relayOut(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
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
			"payment_mode":        "hosted",
			"payment_gateway_url": cfg.PaymentGatewayURL,
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		// Credits and history are best-effort separately: a gateway hiccup
		// on one must not blank the other.
		if bal, rate, err := payer.AccountInfo(ctx); err == nil {
			out["gateway_credit_atto"] = bal
			// Exact USD-per-ANT rate for fiat display (V2-1100).
			if rate != "" {
				out["rate_usd_per_ant"] = rate
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
		relayOut(w, status, raw)
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
		relayOut(w, status, raw)
	}
}
