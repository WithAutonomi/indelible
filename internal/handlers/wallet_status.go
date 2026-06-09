package handlers

import (
	"net/http"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

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

		jsonResponse(w, http.StatusOK, map[string]any{
			"has_default_wallet": hasWallet,
		})
	}
}
