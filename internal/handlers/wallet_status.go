package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/evm"
	"github.com/WithAutonomi/indelible/internal/services"
)

// gatewayPricingCache memoizes the gateway's USD-per-ANT rate (crypto-free
// display, V2-1100) and per-batch network fee (fee-aware estimates, V2-1113):
// every authenticated view reads wallet-status, so the gateway is asked at
// most once per minute.
var gatewayPricingCache struct {
	sync.Mutex
	rate    string
	fee     string
	fetched time.Time
}

func cachedGatewayPricing(ctx context.Context, cfg *config.Config) (rate, feePerBatchAtto string) {
	gatewayPricingCache.Lock()
	defer gatewayPricingCache.Unlock()
	if time.Since(gatewayPricingCache.fetched) < time.Minute {
		return gatewayPricingCache.rate, gatewayPricingCache.fee
	}
	rateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	payer := evm.NewHostedPayer(cfg.PaymentGatewayURL, cfg.PaymentGatewayAPIKey)
	_, rate, fee, err := payer.AccountInfo(rateCtx)
	if err != nil {
		// Best-effort display data: keep serving the stale values and try
		// again after the normal interval.
		gatewayPricingCache.fetched = time.Now()
		return gatewayPricingCache.rate, gatewayPricingCache.fee
	}
	gatewayPricingCache.rate, gatewayPricingCache.fee = rate, fee
	gatewayPricingCache.fetched = time.Now()
	return rate, fee
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
		mode := "local"
		if cfg.PaymentMode == "hosted" {
			mode = "hosted"
		}
		out := map[string]any{
			"has_default_wallet": hasWallet || mode == "hosted",
			"payment_mode":       mode,
		}
		// Crypto-free display (V2-1100): the gateway's USD-per-ANT rate, so
		// every view can render costs and balances in fiat. Best-effort and
		// cached — absent when the gateway has no rate or is unreachable.
		if mode == "hosted" && cfg.PaymentGatewayURL != "" {
			rate, fee := cachedGatewayPricing(r.Context(), cfg)
			if rate != "" {
				out["gateway_rate_usd_per_ant"] = rate
			}
			// Fee-aware estimates (V2-1113): the per-batch network fee the
			// gateway adds to every settled batch (V2-1098). The web app folds
			// it into pre-upload estimates so they match the gross debit.
			// Absent when the gateway charges none or predates the field.
			if fee != "" {
				out["gateway_fee_per_batch_atto"] = fee
			}
		}
		jsonResponse(w, http.StatusOK, out)
	}
}
