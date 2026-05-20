package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// Public-facing shape of an enabled OIDC provider — never exposes the
// client_id or any secret, only what the login page needs to render a
// "Sign in with X" button.
type publicOIDCProvider struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// oidcIdentityResponse is the per-identity shape for the profile
// "connected accounts" view.
type oidcIdentityResponse struct {
	ID           int64  `json:"id"`
	ProviderID   int64  `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Subject      string `json:"subject"`
	CreatedAt    string `json:"created_at"`
}

// ListOIDCProviders godoc
// @Summary List OIDC providers
// @Description Returns enabled OIDC providers for the login page (no secrets, no client_id)
// @Tags Auth
// @Produce json
// @Success 200 {object} map[string][]publicOIDCProvider
// @Router /auth/oidc/providers [get]
func ListOIDCProviders(db *database.DB) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, "")
	return func(w http.ResponseWriter, r *http.Request) {
		providers, err := providerSvc.List()
		if err != nil {
			jsonError(w, "failed to list providers", http.StatusInternalServerError)
			return
		}
		out := make([]publicOIDCProvider, 0, len(providers))
		for _, p := range providers {
			if !p.IsEnabled {
				continue
			}
			out = append(out, publicOIDCProvider{ID: p.ID, Name: p.Name, DisplayName: p.DisplayName})
		}
		jsonResponse(w, http.StatusOK, map[string]any{"providers": out})
	}
}

// OIDCAuthorize godoc
// @Summary Start OIDC login flow
// @Description Generates state/nonce/PKCE, sets an encrypted cookie, and 302's to the IdP authorize endpoint
// @Tags Auth
// @Param providerId path int true "OIDC provider ID"
// @Success 302
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /auth/oidc/authorize/{providerId} [get]
func OIDCAuthorize(db *database.DB, cfg *config.Config) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(db, providerSvc, cfg.WalletEncryptionKey)
	logSvc := services.NewLogService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		providerID, err := strconv.ParseInt(chi.URLParam(r, "providerId"), 10, 64)
		if err != nil {
			jsonError(w, "invalid provider id", http.StatusBadRequest)
			return
		}

		authURL, cookieValue, err := loginSvc.BuildAuthorizeURL(r.Context(), providerID, services.AuthorizeOpts{
			RedirectURL: oidcCallbackURL(r, cfg),
		})
		if err != nil {
			if errors.Is(err, services.ErrOIDCProviderDisabled) {
				jsonError(w, "provider is disabled", http.StatusForbidden)
				return
			}
			if errors.Is(err, services.ErrOIDCProviderNotFound) {
				jsonError(w, "provider not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to build authorize URL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		auditEvent(r, logSvc, "sso_authorize_started", "info", nil, fmt.Sprintf("provider=%d", providerID))
		setOIDCStateCookie(w, r, cookieValue)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// OIDCCallback godoc
// @Summary Complete OIDC login flow
// @Description Validates the IdP response, resolves or provisions the user, issues a session JWT, and redirects to /
// @Tags Auth
// @Param code  query string true  "OIDC authorization code"
// @Param state query string true  "Opaque state token (matches encrypted cookie)"
// @Success 302
// @Failure 400 {object} map[string]string
// @Router /auth/oidc/callback [get]
func OIDCCallback(db *database.DB, cfg *config.Config) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(db, providerSvc, cfg.WalletEncryptionKey)
	userSvc := services.NewUserService(db)
	settingsSvc := services.NewSettingsService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		// Provider-side errors (user clicked Cancel, IdP refused, etc.) are
		// returned with ?error=...&error_description=...  Just relay to /login.
		if idpErr := r.URL.Query().Get("error"); idpErr != "" {
			auditEvent(r, logSvc, "sso_login_failed", "warn", nil, "idp_error: "+idpErr)
			clearOIDCStateCookie(w, r)
			http.Redirect(w, r, "/login?error="+idpErr, http.StatusFound)
			return
		}

		cookie, _ := r.Cookie(services.OIDCStateCookieName)
		clearOIDCStateCookie(w, r)

		cookieValue := ""
		if cookie != nil {
			cookieValue = cookie.Value
		}

		outcome, err := loginSvc.HandleCallback(r.Context(), cookieValue,
			r.URL.Query().Get("state"), r.URL.Query().Get("code"))
		if err != nil {
			auditEvent(r, logSvc, "sso_login_failed", "warn", nil, oidcErrorCode(err))
			http.Redirect(w, r, "/login?error="+oidcErrorCode(err), http.StatusFound)
			return
		}

		// Linking flow — already logged in, just redirect to profile.
		if outcome.LinkedUserID != 0 {
			lid := outcome.LinkedUserID
			auditEvent(r, logSvc, "sso_identity_linked", "info", &lid, "")
			http.Redirect(w, r, "/profile?linked=1", http.StatusFound)
			return
		}

		user := outcome.LoggedInUser
		if user == nil {
			http.Redirect(w, r, "/login?error=internal", http.StatusFound)
			return
		}

		// Issue JWT, set session cookie, redirect to dashboard. Mirrors Login.
		expiryHours := 24
		if v, err := settingsSvc.Get("jwt_expiry_hours"); err == nil {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				expiryHours = n
			}
		}
		token, err := auth.GenerateToken(cfg.JWTSecret, user.ID, user.Email, expiryHours)
		if err != nil {
			http.Redirect(w, r, "/login?error=internal", http.StatusFound)
			return
		}
		_ = userSvc.UpdateLastLogin(user.ID)

		detail := "sso"
		if outcome.IsNewUser {
			detail = "sso auto-provisioned"
		}
		auditEvent(r, logSvc, "sso_login", "info", &user.ID, detail)

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			MaxAge:   expiryHours * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			SameSite: http.SameSiteLaxMode,
		})

		redirect := "/"
		if outcome.IsNewUser {
			redirect = "/?welcome=1"
		}
		http.Redirect(w, r, redirect, http.StatusFound)
	}
}

// OIDCLinkStart godoc
// @Summary Start OIDC linking flow
// @Description Authenticated user kicks off an OIDC flow that adds a new identity to their account on callback
// @Tags Auth
// @Param providerId path int true "OIDC provider ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /auth/oidc/link/{providerId} [post]
func OIDCLinkStart(db *database.DB, cfg *config.Config) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(db, providerSvc, cfg.WalletEncryptionKey)
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == 0 {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		providerID, err := strconv.ParseInt(chi.URLParam(r, "providerId"), 10, 64)
		if err != nil {
			jsonError(w, "invalid provider id", http.StatusBadRequest)
			return
		}

		authURL, cookieValue, err := loginSvc.BuildAuthorizeURL(r.Context(), providerID, services.AuthorizeOpts{
			RedirectURL:  oidcCallbackURL(r, cfg),
			LinkToUserID: userID,
		})
		if err != nil {
			if errors.Is(err, services.ErrOIDCProviderDisabled) {
				jsonError(w, "provider is disabled", http.StatusForbidden)
				return
			}
			if errors.Is(err, services.ErrOIDCProviderNotFound) {
				jsonError(w, "provider not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to build authorize URL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		setOIDCStateCookie(w, r, cookieValue)
		// JSON response so the frontend can window.location.href = authorize_url
		// (POST → redirect doesn't play nicely with most fetch helpers).
		jsonResponse(w, http.StatusOK, map[string]string{"authorize_url": authURL})
	}
}

// OIDCUnlinkIdentity godoc
// @Summary Unlink an OIDC identity
// @Description Remove an OIDC login method from the current user
// @Tags Auth
// @Param identityId path int true "OIDC identity ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /auth/oidc/identities/{identityId} [delete]
func OIDCUnlinkIdentity(db *database.DB, cfg *config.Config) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	loginSvc := services.NewOIDCLoginService(db, providerSvc, cfg.WalletEncryptionKey)
	logSvc := services.NewLogService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == 0 {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		identityID, err := strconv.ParseInt(chi.URLParam(r, "identityId"), 10, 64)
		if err != nil {
			jsonError(w, "invalid identity id", http.StatusBadRequest)
			return
		}
		if err := loginSvc.UnlinkIdentity(identityID, userID); err != nil {
			if errors.Is(err, services.ErrOIDCCannotUnlinkLast) {
				jsonErrorWithCode(w, "cannot unlink your only login method", "last_login_method", http.StatusConflict)
				return
			}
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		auditEvent(r, logSvc, "sso_identity_unlinked", "info", &userID, fmt.Sprintf("identity=%d", identityID))
		w.WriteHeader(http.StatusNoContent)
	}
}

// ListMyOIDCIdentities godoc
// @Summary List my OIDC identities
// @Description Returns the linked OIDC identities for the current user (profile connected accounts)
// @Tags Auth
// @Produce json
// @Success 200 {object} map[string][]oidcIdentityResponse
// @Security BearerAuth
// @Router /me/oidc/identities [get]
func ListMyOIDCIdentities(db *database.DB) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, "")
	loginSvc := services.NewOIDCLoginService(db, providerSvc, "")
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == 0 {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		identities, err := loginSvc.ListIdentitiesForUser(userID)
		if err != nil {
			jsonError(w, "failed to list identities", http.StatusInternalServerError)
			return
		}
		// Pre-fetch provider names so the UI can label rows without an extra round trip.
		providers, _ := providerSvc.List()
		nameByID := make(map[int64]string, len(providers))
		for _, p := range providers {
			nameByID[p.ID] = p.DisplayName
		}
		out := make([]oidcIdentityResponse, 0, len(identities))
		for _, id := range identities {
			out = append(out, oidcIdentityResponse{
				ID:           id.ID,
				ProviderID:   id.ProviderID,
				ProviderName: nameByID[id.ProviderID],
				Subject:      id.Subject,
				CreatedAt:    id.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		jsonResponse(w, http.StatusOK, map[string]any{"identities": out})
	}
}

// --- helpers ---------------------------------------------------------------

// oidcCallbackURL builds the absolute callback URL the IdP should redirect to.
// Prefers cfg.BaseURL when set (production), falls back to deriving from the
// incoming request (dev / single-host).
func oidcCallbackURL(r *http.Request, cfg *config.Config) string {
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/") + "/api/v2/auth/oidc/callback"
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/v2/auth/oidc/callback"
}

func setOIDCStateCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     services.OIDCStateCookieName,
		Value:    value,
		Path:     "/api/v2/auth/oidc",
		MaxAge:   int(services.OIDCStateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		// Lax (not Strict) so the IdP-back redirect survives the cross-site hop.
		SameSite: http.SameSiteLaxMode,
	})
}

func clearOIDCStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     services.OIDCStateCookieName,
		Value:    "",
		Path:     "/api/v2/auth/oidc",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

// oidcErrorCode maps service-layer sentinels to the short error codes the
// frontend can interpret on /login?error=… without leaking internals.
func oidcErrorCode(err error) string {
	switch {
	case errors.Is(err, services.ErrOIDCNoAccount):
		return "no_account"
	case errors.Is(err, services.ErrOIDCEmailCollision):
		return "email_exists"
	case errors.Is(err, services.ErrOIDCStateCookieMissing),
		errors.Is(err, services.ErrOIDCStateExpired),
		errors.Is(err, services.ErrOIDCStateMismatch),
		errors.Is(err, services.ErrOIDCNonceMismatch):
		return "session_expired"
	case errors.Is(err, services.ErrOIDCMissingEmail):
		return "missing_email"
	case errors.Is(err, services.ErrOIDCProviderDisabled):
		return "provider_disabled"
	default:
		return "internal"
	}
}

// --- Admin: set auto_provision + default_group_id --------------------------

type adminOIDCAutoProvisionRequest struct {
	AutoProvision  bool  `json:"auto_provision"`
	DefaultGroupID int64 `json:"default_group_id"`
}

// AdminSetOIDCAutoProvision godoc
// @Summary Toggle OIDC auto-provisioning
// @Description Configure whether unknown sub/email pairs create new local users (off by default), and which group new users join
// @Tags Admin: OIDC
// @Accept json
// @Produce json
// @Param id path int true "Provider ID"
// @Param body body adminOIDCAutoProvisionRequest true "auto_provision + default_group_id (0 to clear)"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /admin/oidc/providers/{id}/auto-provision [put]
func AdminSetOIDCAutoProvision(db *database.DB, cfg *config.Config) http.HandlerFunc {
	providerSvc := services.NewOIDCProviderService(db, cfg.WalletEncryptionKey)
	logSvc := services.NewLogService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid provider id", http.StatusBadRequest)
			return
		}
		var req adminOIDCAutoProvisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := providerSvc.SetAutoProvision(id, req.AutoProvision, req.DefaultGroupID); err != nil {
			jsonError(w, "failed to update provider: "+err.Error(), http.StatusInternalServerError)
			return
		}
		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "oidc_auto_provision_changed", "info", &callerID,
			fmt.Sprintf("provider=%d auto_provision=%t default_group=%d", id, req.AutoProvision, req.DefaultGroupID))
		jsonResponse(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
