package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/services"
)

type walletResponse struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Address        string `json:"address"`
	IsDefault      bool   `json:"is_default"`
	PaymentBalance string `json:"payment_balance"`
	GasBalance     string `json:"gas_balance"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func toWalletResponse(w *services.Wallet) walletResponse {
	return walletResponse{
		ID:             w.ID,
		Name:           w.Name,
		Address:        w.Address,
		IsDefault:      w.IsDefault,
		PaymentBalance: w.PaymentBalance,
		GasBalance:     w.GasBalance,
		CreatedAt:      w.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      w.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

type createWalletRequest struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"`
}

// @Summary      List all wallets
// @Description  Return all configured wallets with their balances
// @Tags         Admin: Wallets
// @Produce      json
// @Success      200 {object} map[string][]walletResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/wallets [get]
// @Security     BearerAuth
// AdminListWallets returns all wallets.
func AdminListWallets(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		wallets, err := walletSvc.List()
		if err != nil {
			jsonError(w, "failed to list wallets", http.StatusInternalServerError)
			return
		}

		resp := make([]walletResponse, 0, len(wallets))
		for _, wl := range wallets {
			resp = append(resp, toWalletResponse(wl))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"wallets": resp})
	}
}

// @Summary      Create a wallet
// @Description  Add a new wallet with encrypted private key storage
// @Tags         Admin: Wallets
// @Accept       json
// @Produce      json
// @Param        body body createWalletRequest true "Wallet details including private key"
// @Success      201 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/wallets [post]
// @Security     BearerAuth
// AdminCreateWallet adds a new wallet with encrypted key storage.
func AdminCreateWallet(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createWalletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Address == "" || req.PrivateKey == "" {
			jsonError(w, "name, address, and private_key are required", http.StatusBadRequest)
			return
		}

		wallet, err := walletSvc.Create(req.Name, req.Address, req.PrivateKey)
		if err != nil {
			jsonError(w, "failed to create wallet", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, map[string]any{
			"message": "wallet created",
			"wallet":  toWalletResponse(wallet),
		})
	}
}

// @Summary      Set default wallet
// @Description  Make a wallet the default for upload payments
// @Tags         Admin: Wallets
// @Produce      json
// @Param        id path int true "Wallet ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/wallets/{id}/default [put]
// @Security     BearerAuth
// AdminSetDefaultWallet makes a wallet the default for uploads.
func AdminSetDefaultWallet(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			jsonError(w, "invalid wallet id", http.StatusBadRequest)
			return
		}

		if err := walletSvc.SetDefault(id); err != nil {
			if errors.Is(err, services.ErrWalletNotFound) {
				jsonError(w, "wallet not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to set default wallet", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "default wallet updated"})
	}
}
