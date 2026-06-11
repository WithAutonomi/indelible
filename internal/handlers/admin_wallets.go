package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/evm"
	"github.com/WithAutonomi/indelible/internal/middleware"
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
	Address    string `json:"address"` // optional — derived from private_key if omitted
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
	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())

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
	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())
	logSvc := services.NewLogService(db)

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

		callerID := middleware.GetUserID(r.Context())
		// Detail records the public address only. NEVER the private key.
		auditEvent(r, logSvc, "wallet_created", "info", &callerID,
			fmt.Sprintf("id=%d name=%s address=%s", wallet.ID, wallet.Name, wallet.Address))

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
	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())
	logSvc := services.NewLogService(db)

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

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "wallet_default_changed", "info", &callerID, fmt.Sprintf("id=%d", id))

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
	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())
	logSvc := services.NewLogService(db)

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

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "wallet_deleted", "warn", &callerID, fmt.Sprintf("id=%d", id))

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
	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())

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
			jsonError(w, "EVM not configured — start indelible with --network arbitrum-one (mainnet) or --network arbitrum-sepolia (testnet), or set INDELIBLE_EVM_RPC_URL and INDELIBLE_EVM_TOKEN_ADDRESS explicitly", http.StatusServiceUnavailable)
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

// walletTransactionResponse mirrors services.Transaction with nullable
// handling for the wire format.
type walletTransactionResponse struct {
	ID           int64   `json:"id"`
	WalletID     int64   `json:"wallet_id"`
	UploadID     *int64  `json:"upload_id"`
	TxType       string  `json:"tx_type"`
	Amount       string  `json:"amount"`
	BalanceAfter string  `json:"balance_after"`
	TxHash       *string `json:"tx_hash"`
	CreatedAt    string  `json:"created_at"`
}

func toWalletTransactionResponse(t *services.Transaction) walletTransactionResponse {
	r := walletTransactionResponse{
		ID:           t.ID,
		WalletID:     t.WalletID,
		TxType:       t.TxType,
		Amount:       t.Amount,
		BalanceAfter: t.BalanceAfter,
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if t.UploadID.Valid {
		r.UploadID = &t.UploadID.Int64
	}
	if t.TxHash.Valid {
		r.TxHash = &t.TxHash.String
	}
	return r
}

// @Summary      Per-wallet transaction history
// @Description  List recorded transactions for a wallet (upload payments, refunds), newest first. V2-321.
// @Tags         Admin: Wallets
// @Produce      json
// @Param        id     path  int true "Wallet ID"
// @Param        limit  query int false "Max results (default 50, max 100)"
// @Param        offset query int false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/wallets/{id}/transactions [get]
// @Security     BearerAuth
// AdminWalletTransactions returns the transaction log for a wallet (V2-321).
func AdminWalletTransactions(db *database.DB) http.HandlerFunc {
	txSvc := services.NewTransactionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid wallet id", http.StatusBadRequest)
			return
		}

		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

		txns, total, err := txSvc.ListByWallet(id, limit, offset)
		if err != nil {
			jsonError(w, "failed to list transactions", http.StatusInternalServerError)
			return
		}

		resp := make([]walletTransactionResponse, 0, len(txns))
		for _, t := range txns {
			resp = append(resp, toWalletTransactionResponse(t))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"transactions": resp,
			"total":        total,
			"limit":        limit,
			"offset":       offset,
		})
	}
}

// crossWalletTransactionResponse adds the owning wallet's name to a transaction
// row, for the "all wallets" view where rows span wallets.
type crossWalletTransactionResponse struct {
	walletTransactionResponse
	WalletName string `json:"wallet_name"`
}

// @Summary      Cross-wallet transaction history
// @Description  List transactions across all wallets with optional filters (wallet_id, type, from/to date range), newest first. Powers the dedicated Transactions page. from/to accept RFC3339 or YYYY-MM-DD. V2-447.
// @Tags         Admin: Wallets
// @Produce      json
// @Param        wallet_id query int    false "Filter to a single wallet"
// @Param        type      query string false "Filter by tx type (e.g. upload, refund)"
// @Param        from      query string false "Start of created_at range (RFC3339 or YYYY-MM-DD)"
// @Param        to        query string false "End of created_at range (RFC3339 or YYYY-MM-DD)"
// @Param        limit     query int    false "Max results (default 50, max 100)"
// @Param        offset    query int    false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/transactions [get]
// @Security     BearerAuth
func AdminCrossWalletTransactions(db *database.DB) http.HandlerFunc {
	txSvc := services.NewTransactionService(db)
	walletSvc := services.NewWalletService(db, nil) // List() is a pure read — no keyring needed

	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		var walletID *int64
		if v := q.Get("wallet_id"); v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				jsonError(w, "invalid wallet_id", http.StatusBadRequest)
				return
			}
			walletID = &id
		}

		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))

		txns, total, err := txSvc.List(walletID, q.Get("type"), parseTimeParam(q.Get("from")), parseTimeParam(q.Get("to")), limit, offset)
		if err != nil {
			jsonError(w, "failed to list transactions", http.StatusInternalServerError)
			return
		}

		// Wallet id→name map for the cross-wallet view. Best effort: rows still
		// render (with blank names) if the lookup fails.
		nameByID := map[int64]string{}
		if wallets, err := walletSvc.List(); err == nil {
			for _, wl := range wallets {
				nameByID[wl.ID] = wl.Name
			}
		}

		resp := make([]crossWalletTransactionResponse, 0, len(txns))
		for _, t := range txns {
			resp = append(resp, crossWalletTransactionResponse{
				walletTransactionResponse: toWalletTransactionResponse(t),
				WalletName:                nameByID[t.WalletID],
			})
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"transactions": resp,
			"total":        total,
			"limit":        limit,
			"offset":       offset,
		})
	}
}
