package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/evm"
	"github.com/WithAutonomi/indelible/internal/services"
)

// gatewayPricingCache memoizes the gateway's USD-per-ANT rate (crypto-free
// display, V2-1100), per-batch network fee (fee-aware estimates, V2-1113)
// and cost-per-GB estimate + basis (capacity display, V2-1114): every
// authenticated view reads wallet-status, so the gateway is asked at most
// once per minute.
//
// Entries are keyed by gateway identity (URL + API key), never process-wide:
// two Configs in one process, or one Config re-pointed at another gateway,
// must never serve each other's numbers (#163 review, V2-1269).
var gatewayPricingCache struct {
	sync.Mutex
	byGateway map[string]*gatewayPricingEntry
}

type gatewayPricingEntry struct {
	pricing gatewayPricing
	fetched time.Time
}

// gatewayPricingTTL bounds how often any one gateway is asked.
const gatewayPricingTTL = time.Minute

// gatewayPricing is the cached display trio+basis; every field optional
// (older gateway, nothing configured, thin history — all read as absent).
type gatewayPricing struct {
	rate      string
	fee       string
	estCostGB string
	estBasis  json.RawMessage
}

func cachedGatewayPricing(ctx context.Context, cfg *config.Config) gatewayPricing {
	key := cfg.PaymentGatewayURL + "\x00" + cfg.PaymentGatewayAPIKey
	gatewayPricingCache.Lock()
	defer gatewayPricingCache.Unlock()
	if gatewayPricingCache.byGateway == nil {
		gatewayPricingCache.byGateway = map[string]*gatewayPricingEntry{}
	}
	e := gatewayPricingCache.byGateway[key]
	if e == nil {
		e = &gatewayPricingEntry{}
		gatewayPricingCache.byGateway[key] = e
	}
	if !e.fetched.IsZero() && time.Since(e.fetched) < gatewayPricingTTL {
		return e.pricing
	}
	rateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	payer := evm.NewHostedPayer(cfg.PaymentGatewayURL, cfg.PaymentGatewayAPIKey)
	d, err := payer.AccountDetails(rateCtx)
	if err != nil {
		// Best-effort display data: keep serving the stale values and try
		// again after the normal interval.
		e.fetched = time.Now()
		return e.pricing
	}
	e.pricing = gatewayPricing{
		rate: d.RateUSDPerANT, fee: d.FeePerBatchAtto,
		estCostGB: d.EstCostPerGBAtto, estBasis: d.EstCostPerGBBasis,
	}
	e.fetched = time.Now()
	return e.pricing
}

// WalletStatus godoc
// @Summary Check wallet configuration status
// @Description Returns whether a default wallet is configured for uploads
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /system/wallet-status [get]
// @Security BearerAuth
func WalletStatus(db *database.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())

	return func(w http.ResponseWriter, r *http.Request) {
		wallet, err := walletSvc.GetDefault()
		hasWallet := err == nil && wallet != nil

		// The UI reads this as "can this instance pay for uploads". Hosted
		// mode pays via the gateway with no wallet at all (V2-929).
		hosted := cfg.PaymentBackend.Hosted()
		out := map[string]any{
			"has_default_wallet": hasWallet || hosted,
			"payment_backend":    string(cfg.PaymentBackend),
		}
		// Crypto-free display (V2-1100): the gateway's USD-per-ANT rate, so
		// every view can render costs and balances in fiat. Best-effort and
		// cached — absent when the gateway has no rate or is unreachable.
		if hosted && cfg.PaymentGatewayURL != "" {
			p := cachedGatewayPricing(r.Context(), cfg)
			if p.rate != "" {
				out["gateway_rate_usd_per_ant"] = p.rate
			}
			// Fee-aware estimates (V2-1113): the per-batch network fee the
			// gateway adds to every settled batch (V2-1098). The web app folds
			// it into pre-upload estimates so they match the gross debit.
			// Absent when the gateway charges none or predates the field.
			if p.fee != "" {
				out["gateway_fee_per_batch_atto"] = p.fee
			}
			// Capacity display (V2-1114): the gateway's directional
			// cost-per-GB estimate and its methodology basis. Absent when the
			// gateway predates the field or has too little paid history —
			// the UI hides the "≈ N GB remaining" line entirely.
			if p.estCostGB != "" {
				out["gateway_est_cost_per_gb_atto"] = p.estCostGB
				if len(p.estBasis) > 0 {
					out["gateway_est_cost_per_gb_basis"] = p.estBasis
				}
			}
		}
		jsonResponse(w, http.StatusOK, out)
	}
}
