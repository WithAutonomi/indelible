package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/evm"
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
	Address    string `json:"address"` // optional â€” derived from private_key if omitted
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
func AdminListWallets(db *database.DB, cfg *config.Config) http.HandlerFunc {
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
func AdminCreateWallet(db *database.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createWalletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.PrivateKey == "" {
			jsonError(w, "name and private_key are required", http.StatusBadRequest)
			return
		}

		// Derive address from private key if not provided
		if req.Address == "" {
			keyHex := strings.TrimPrefix(req.PrivateKey, "0x")
			privKey, err := crypto.HexToECDSA(keyHex)
			if err != nil {
				jsonError(w, "invalid private key", http.StatusBadRequest)
				return
			}
			req.Address = crypto.PubkeyToAddress(privKey.PublicKey).Hex()
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
func AdminSetDefaultWallet(db *database.DB, cfg *config.Config) http.HandlerFunc {
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

// @Summary      Delete a wallet
// @Description  Remove a wallet (cannot delete the default wallet)
// @Tags         Admin: Wallets
// @Produce      json
// @Param        id path int true "Wallet ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Router       /admin/wallets/{id} [delete]
// @Security     BearerAuth
func AdminDeleteWallet(db *database.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			jsonError(w, "invalid wallet id", http.StatusBadRequest)
			return
		}

		if err := walletSvc.Delete(id); err != nil {
			if errors.Is(err, services.ErrWalletNotFound) {
				jsonError(w, "wallet not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, services.ErrDeleteDefault) {
				jsonError(w, err.Error(), http.StatusConflict)
				return
			}
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "wallet deleted"})
	}
}

// @Summary      Refresh wallet balance
// @Description  Query the EVM chain for the current token and gas balance
// @Tags         Admin: Wallets
// @Produce      json
// @Param        id path int true "Wallet ID"
// @Success      200 {object} map[string]interface{}
// @Failure      404 {object} map[string]string
// @Router       /admin/wallets/{id}/balance [post]
// @Security     BearerAuth
func AdminRefreshWalletBalance(db *database.DB, cfg *config.Config) http.HandlerFunc {
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			jsonError(w, "invalid wallet id", http.StatusBadRequest)
			return
		}

		wallet, err := walletSvc.GetByID(id)
		if err != nil {
			if errors.Is(err, services.ErrWalletNotFound) {
				jsonError(w, "wallet not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to get wallet", http.StatusInternalServerError)
			return
		}

		if cfg.EvmRPCURL == "" || cfg.EvmTokenAddress == "" {
			jsonError(w, "EVM not configured â€” start indelible with --network arbitrum-one (mainnet) or --network arbitrum-sepolia (testnet), or set INDELIBLE_EVM_RPC_URL and INDELIBLE_EVM_TOKEN_ADDRESS explicitly", http.StatusServiceUnavailable)
			return
		}

		signer, err := evm.NewSigner(cfg.EvmRPCURL)
		if err != nil {
			jsonError(w, "failed to connect to EVM RPC", http.StatusBadGateway)
			return
		}
		defer signer.Close()

		tokenBal, gasBal, err := signer.GetBalances(r.Context(), wallet.Address, cfg.EvmTokenAddress)
		if err != nil {
			jsonError(w, "failed to query balance: "+err.Error(), http.StatusBadGateway)
			return
		}

		_ = walletSvc.UpdateBalance(id, tokenBal, gasBal)

		jsonResponse(w, http.StatusOK, map[string]any{
			"payment_balance": tokenBal,
			"gas_balance":     gasBal,
		})
	}
}
