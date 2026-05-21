package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type oidcProviderResponse struct {
	ID                   int64             `json:"id"`
	Name                 string            `json:"name"`
	DisplayName          string            `json:"display_name"`
	IssuerURL            string            `json:"issuer_url"`
	ClientID             string            `json:"client_id"`
	Scopes               string            `json:"scopes"`
	IsEnabled            bool              `json:"is_enabled"`
	AutoProvision        bool              `json:"auto_provision"`
	DefaultGroupID       *int64            `json:"default_group_id"`
	ExtraAuthorizeParams map[string]string `json:"extra_authorize_params"`
	CreatedAt            string            `json:"created_at"`
	UpdatedAt            string            `json:"updated_at"`
}

func toOIDCProviderResponse(p *services.OIDCProvider) oidcProviderResponse {
	// Always serialize the params field as an object so the frontend can use a
	// stable shape (Object.entries, etc.). nil → {} avoids JSON null on the wire.
	params := p.ExtraAuthorizeParams
	if params == nil {
		params = map[string]string{}
	}
	resp := oidcProviderResponse{
		ID:                   p.ID,
		Name:                 p.Name,
		DisplayName:          p.DisplayName,
		IssuerURL:            p.IssuerURL,
		ClientID:             p.ClientID,
		Scopes:               p.Scopes,
		IsEnabled:            p.IsEnabled,
		AutoProvision:        p.AutoProvision,
		ExtraAuthorizeParams: params,
		CreatedAt:            p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:            p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if p.DefaultGroupID.Valid {
		gid := p.DefaultGroupID.Int64
		resp.DefaultGroupID = &gid
	}
	return resp
}

type createOIDCRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	IssuerURL    string `json:"issuer_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scopes       string `json:"scopes"`
}

type updateOIDCRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	IssuerURL    string `json:"issuer_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // empty = keep existing
	Scopes       string `json:"scopes"`
	IsEnabled    bool   `json:"is_enabled"`
}

// @Summary      List OIDC providers
// @Description  Return all configured OIDC identity providers
// @Tags         Admin: OIDC
// @Produce      json
// @Success      200 {object} map[string][]oidcProviderResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/oidc/providers [get]
// @Security     BearerAuth
func AdminListOIDCProviders(db *database.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		providers, err := oidcSvc.List()
		if err != nil {
			jsonError(w, "failed to list OIDC providers", http.StatusInternalServerError)
			return
		}

		resp := make([]oidcProviderResponse, 0, len(providers))
		for _, p := range providers {
			resp = append(resp, toOIDCProviderResponse(p))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"providers": resp})
	}
}

// @Summary      Create an OIDC provider
// @Description  Register a new OIDC identity provider with encrypted client secret
// @Tags         Admin: OIDC
// @Accept       json
// @Produce      json
// @Param        body body createOIDCRequest true "OIDC provider details"
// @Success      201 {object} oidcProviderResponse
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/oidc/providers [post]
// @Security     BearerAuth
func AdminCreateOIDCProvider(db *database.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createOIDCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.IssuerURL == "" || req.ClientID == "" || req.ClientSecret == "" {
			jsonError(w, "name, issuer_url, client_id, and client_secret are required", http.StatusBadRequest)
			return
		}
		if req.DisplayName == "" {
			req.DisplayName = req.Name
		}

		provider, err := oidcSvc.Create(req.Name, req.DisplayName, req.IssuerURL, req.ClientID, req.ClientSecret, req.Scopes)
		if err != nil {
			jsonError(w, "failed to create OIDC provider", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		// Detail logs the issuer URL + client_id (public-ish identifiers) so
		// an operator can correlate the audit row with the provider config in
		// the IdP console. NEVER logs req.ClientSecret.
		auditEvent(r, logSvc, "oidc_provider_created", "info", &callerID,
			fmt.Sprintf("id=%d name=%s issuer=%s client_id=%s", provider.ID, provider.Name, provider.IssuerURL, provider.ClientID))

		jsonResponse(w, http.StatusCreated, toOIDCProviderResponse(provider))
	}
}

// @Summary      Update an OIDC provider
// @Description  Update an existing OIDC provider's configuration (empty client_secret keeps existing)
// @Tags         Admin: OIDC
// @Accept       json
// @Produce      json
// @Param        id   path int               true "Provider ID"
// @Param        body body updateOIDCRequest  true "Updated provider fields"
// @Success      200 {object} oidcProviderResponse
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/oidc/providers/{id} [put]
// @Security     BearerAuth
func AdminUpdateOIDCProvider(db *database.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid provider id", http.StatusBadRequest)
			return
		}

		var req updateOIDCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Snapshot pre-state for the enabled/disabled transition event.
		pre, _ := oidcSvc.GetByID(id)

		provider, err := oidcSvc.Update(id, req.Name, req.DisplayName, req.IssuerURL, req.ClientID, req.ClientSecret, req.Scopes, req.IsEnabled)
		if err != nil {
			if errors.Is(err, services.ErrOIDCProviderNotFound) {
				jsonError(w, "OIDC provider not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to update OIDC provider", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "oidc_provider_updated", "info", &callerID, fmt.Sprintf("id=%d", id))
		if pre != nil && pre.IsEnabled != provider.IsEnabled {
			event := "oidc_provider_enabled"
			if !provider.IsEnabled {
				event = "oidc_provider_disabled"
			}
			auditEvent(r, logSvc, event, "info", &callerID, fmt.Sprintf("id=%d", id))
		}

		jsonResponse(w, http.StatusOK, toOIDCProviderResponse(provider))
	}
}

// @Summary      Delete an OIDC provider
// @Description  Remove an OIDC identity provider
// @Tags         Admin: OIDC
// @Produce      json
// @Param        id path int true "Provider ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/oidc/providers/{id} [delete]
// @Security     BearerAuth
func AdminDeleteOIDCProvider(db *database.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid provider id", http.StatusBadRequest)
			return
		}

		if err := oidcSvc.Delete(id); err != nil {
			if errors.Is(err, services.ErrOIDCProviderNotFound) {
				jsonError(w, "OIDC provider not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to delete OIDC provider", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "oidc_provider_deleted", "warn", &callerID, fmt.Sprintf("id=%d", id))

		jsonResponse(w, http.StatusOK, map[string]string{"message": "OIDC provider deleted"})
	}
}
