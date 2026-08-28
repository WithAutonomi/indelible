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

// gatewayRateCache memoizes the gateway's USD-per-ANT rate for the
// crypto-free display (V2-1100): every authenticated view reads
// wallet-status, so the gateway is asked at most once per minute.
var gatewayRateCache struct {
	sync.Mutex
	rate    string
	fetched time.Time
}

func cachedGatewayRate(ctx context.Context, cfg *config.Config) string {
	gatewayRateCache.Lock()
	defer gatewayRateCache.Unlock()
	if time.Since(gatewayRateCache.fetched) < time.Minute {
		return gatewayRateCache.rate
	}
	rateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	payer := evm.NewHostedPayer(cfg.PaymentGatewayURL, cfg.PaymentGatewayAPIKey)
	_, rate, err := payer.AccountInfo(rateCtx)
	if err != nil {
		// Best-effort display data: keep serving the stale value and try
		// again after the normal interval.
		gatewayRateCache.fetched = time.Now()
		return gatewayRateCache.rate
	}
	gatewayRateCache.rate, gatewayRateCache.fetched = rate, time.Now()
	return rate
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
			if rate := cachedGatewayRate(r.Context(), cfg); rate != "" {
				out["gateway_rate_usd_per_ant"] = rate
			}
		}
		jsonResponse(w, http.StatusOK, out)
	}
}
