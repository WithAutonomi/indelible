package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/services"
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
