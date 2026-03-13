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

type oidcProviderResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	IssuerURL   string `json:"issuer_url"`
	ClientID    string `json:"client_id"`
	Scopes      string `json:"scopes"`
	IsEnabled   bool   `json:"is_enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toOIDCProviderResponse(p *services.OIDCProvider) oidcProviderResponse {
	return oidcProviderResponse{
		ID:          p.ID,
		Name:        p.Name,
		DisplayName: p.DisplayName,
		IssuerURL:   p.IssuerURL,
		ClientID:    p.ClientID,
		Scopes:      p.Scopes,
		IsEnabled:   p.IsEnabled,
		CreatedAt:   p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
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

func AdminListOIDCProviders(db *sql.DB, cfg *config.Config) http.HandlerFunc {
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

func AdminCreateOIDCProvider(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)

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

		jsonResponse(w, http.StatusCreated, toOIDCProviderResponse(provider))
	}
}

func AdminUpdateOIDCProvider(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)

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

		provider, err := oidcSvc.Update(id, req.Name, req.DisplayName, req.IssuerURL, req.ClientID, req.ClientSecret, req.Scopes, req.IsEnabled)
		if err != nil {
			if errors.Is(err, services.ErrOIDCProviderNotFound) {
				jsonError(w, "OIDC provider not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to update OIDC provider", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, toOIDCProviderResponse(provider))
	}
}

func AdminDeleteOIDCProvider(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	oidcSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)

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

		jsonResponse(w, http.StatusOK, map[string]string{"message": "OIDC provider deleted"})
	}
}
