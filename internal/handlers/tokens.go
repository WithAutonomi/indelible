package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type createTokenRequest struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Permissions      []string `json:"permissions"` // ["read"], ["read","write"], etc.
	Department       string   `json:"department"`
	ExpiresInDays    *int     `json:"expires_in_days"`     // null = use system default
	MaxFileSizeBytes *int64   `json:"max_file_size_bytes"` // null = inherit user/system limit
	AllowedFileTypes []string `json:"allowed_file_types"`  // empty = inherit user/system list
}

type tokenResponse struct {
	UUID             string   `json:"uuid"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Permissions      string   `json:"permissions"`
	Department       *string  `json:"department"`
	MaxFileSizeBytes *int64   `json:"max_file_size_bytes"`
	AllowedFileTypes []string `json:"allowed_file_types"`
	UsageCount       int64    `json:"usage_count"`
	LastUsedAt       *string  `json:"last_used_at"`
	ExpiresAt        *string  `json:"expires_at"`
	RevokedAt        *string  `json:"revoked_at"`
	RevokedBy        *int64   `json:"revoked_by"`
	RevokeReason     *string  `json:"revoke_reason"`
	CreatedAt        string   `json:"created_at"`
	OwnerID          int64    `json:"owner_id"`
}

type createTokenResponse struct {
	Secret string        `json:"secret"` // shown once
	Token  tokenResponse `json:"token"`
}

type revokeTokenRequest struct {
	Reason string `json:"reason"`
}

type bulkRevokeRequest struct {
	TokenUUIDs []string `json:"token_uuids"`
	Reason     string   `json:"reason"`
}

func toTokenResponse(t *services.Token) tokenResponse {
	r := tokenResponse{
		UUID:             t.UUID,
		Name:             t.Name,
		Description:      t.Description,
		Permissions:      t.Permissions,
		AllowedFileTypes: []string{},
		UsageCount:       t.UsageCount,
		CreatedAt:        t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		OwnerID:          t.UserID,
	}
	if t.Department.Valid {
		r.Department = &t.Department.String
	}
	if t.MaxFileSizeBytes.Valid {
		v := t.MaxFileSizeBytes.Int64
		r.MaxFileSizeBytes = &v
	}
	if t.AllowedFileTypes.Valid && t.AllowedFileTypes.String != "" {
		var types []string
		if err := json.Unmarshal([]byte(t.AllowedFileTypes.String), &types); err == nil {
			r.AllowedFileTypes = types
		}
	}
	if t.LastUsedAt.Valid {
		s := t.LastUsedAt.Time.Format("2006-01-02T15:04:05Z")
		r.LastUsedAt = &s
	}
	if t.ExpiresAt.Valid {
		s := t.ExpiresAt.Time.Format("2006-01-02T15:04:05Z")
		r.ExpiresAt = &s
	}
	if t.RevokedAt.Valid {
		s := t.RevokedAt.Time.Format("2006-01-02T15:04:05Z")
		r.RevokedAt = &s
	}
	if t.RevokedBy.Valid {
		v := t.RevokedBy.Int64
		r.RevokedBy = &v
	}
	if t.RevokeReason.Valid {
		v := t.RevokeReason.String
		r.RevokeReason = &v
	}
	return r
}

// --- User token handlers (own tokens) ---

// CreateToken creates a new API token for the authenticated user.
//
// @Summary      Create API token
// @Description  Create a new API token with specified permissions and expiry
// @Tags         API Tokens
// @Accept       json
// @Produce      json
// @Param        body  body  createTokenRequest  true  "Token creation request"
// @Success      201  {object}  createTokenResponse
// @Failure      400  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /tokens [post]
func CreateToken(db *database.DB, cfg *config.Config) http.HandlerFunc {
	tokenSvc := services.NewTokenService(db)
	permSvc := services.NewPermissionService(db)
	settingsSvc := services.NewSettingsService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var req createTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}

		// Validate requested permissions
		if len(req.Permissions) == 0 {
			req.Permissions = []string{"read"}
		}
		for _, p := range req.Permissions {
			if p != "read" && p != "write" && p != "admin" {
				jsonError(w, "permissions must be read, write, or admin", http.StatusBadRequest)
				return
			}
		}

		// Non-admin users can only create read/write tokens
		isAdmin, _ := permSvc.IsAdmin(userID)
		for _, p := range req.Permissions {
			if p == "admin" && !isAdmin {
				jsonError(w, "only admins can create admin tokens", http.StatusForbidden)
				return
			}
		}

		// Calculate expiry
		var expiresAt *time.Time
		defaultDays := 90
		if v, err := settingsSvc.Get("default_token_expiry_days"); err == nil {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				defaultDays = n
			}
		}
		days := defaultDays
		if req.ExpiresInDays != nil {
			days = *req.ExpiresInDays
		}
		if days > 0 && days <= 3650 {
			t := time.Now().AddDate(0, 0, days)
			expiresAt = &t
		}

		permsJSON, _ := json.Marshal(req.Permissions)

		// Restrictions: 0 or negative max means "no per-token limit" (inherit).
		var maxFileSize *int64
		if req.MaxFileSizeBytes != nil && *req.MaxFileSizeBytes > 0 {
			maxFileSize = req.MaxFileSizeBytes
		}
		var allowedTypesJSON string
		if len(req.AllowedFileTypes) > 0 {
			b, _ := json.Marshal(req.AllowedFileTypes)
			allowedTypesJSON = string(b)
		}

		secret, token, err := tokenSvc.Create(
			userID, req.Name, req.Description,
			string(permsJSON), req.Department,
			maxFileSize, allowedTypesJSON, expiresAt,
		)
		if err != nil {
			jsonError(w, "failed to create token", http.StatusInternalServerError)
			return
		}

		// Audit the issuance. NEVER includes the secret. Detail records the
		// token UUID + scopes so an incident-response operator can correlate.
		auditEvent(r, logSvc, "api_token_issued", "info", &userID,
			fmt.Sprintf("token=%s scopes=%s", token.UUID, string(permsJSON)))

		jsonResponse(w, http.StatusCreated, createTokenResponse{
			Secret: secret,
			Token:  toTokenResponse(token),
		})
	}
}

// ListTokens returns all API tokens belonging to the authenticated user.
//
// @Summary      List user's tokens
// @Description  List all API tokens owned by the authenticated user
// @Tags         API Tokens
// @Produce      json
// @Success      200  {object}  map[string]any
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /tokens [get]
func ListTokens(db *database.DB) http.HandlerFunc {
	tokenSvc := services.NewTokenService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		tokens, err := tokenSvc.ListByUser(userID)
		if err != nil {
			jsonError(w, "failed to list tokens", http.StatusInternalServerError)
			return
		}

		resp := make([]tokenResponse, 0, len(tokens))
		for _, t := range tokens {
			resp = append(resp, toTokenResponse(t))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"tokens": resp})
	}
}

// RevokeToken revokes an API token owned by the authenticated user.
//
// @Summary      Revoke token
// @Description  Revoke an API token by its UUID
// @Tags         API Tokens
// @Accept       json
// @Produce      json
// @Param        id  path  string  true  "Token UUID"
// @Success      200  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /tokens/{id} [delete]
func RevokeToken(db *database.DB) http.HandlerFunc {
	tokenSvc := services.NewTokenService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		tokenUUID := chi.URLParam(r, "id")

		var req revokeTokenRequest
		// Body is optional for revoke
		_ = json.NewDecoder(r.Body).Decode(&req)

		token, err := tokenSvc.GetByUUID(tokenUUID)
		if err != nil {
			if errors.Is(err, services.ErrTokenNotFound) {
				jsonError(w, "token not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to find token", http.StatusInternalServerError)
			return
		}

		// Users can only revoke their own tokens
		if token.UserID != userID {
			jsonError(w, "token not found", http.StatusNotFound)
			return
		}

		if err := tokenSvc.Revoke(token.ID, userID, req.Reason); err != nil {
			jsonError(w, "failed to revoke token", http.StatusInternalServerError)
			return
		}

		auditEvent(r, logSvc, "api_token_revoked", "info", &userID,
			fmt.Sprintf("token=%s reason=%s", token.UUID, req.Reason))

		jsonResponse(w, http.StatusOK, map[string]string{"message": "token revoked"})
	}
}

// --- Admin token handlers ---

// @Summary      List all tokens (admin)
// @Description  Return a paginated list of all API tokens across all users
// @Tags         Admin: Tokens
// @Produce      json
// @Param        limit  query int false "Max results (default 50, max 100)"
// @Param        offset query int false "Offset for pagination"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/tokens [get]
// @Security     BearerAuth
func AdminListAllTokens(db *database.DB) http.HandlerFunc {
	tokenSvc := services.NewTokenService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 || limit > 100 {
			limit = 50
		}

		tokens, total, err := tokenSvc.ListAll(limit, offset)
		if err != nil {
			jsonError(w, "failed to list tokens", http.StatusInternalServerError)
			return
		}

		resp := make([]tokenResponse, 0, len(tokens))
		for _, t := range tokens {
			resp = append(resp, toTokenResponse(t))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"tokens": resp,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// @Summary      Bulk revoke tokens
// @Description  Revoke multiple API tokens at once by their UUIDs
// @Tags         Admin: Tokens
// @Accept       json
// @Produce      json
// @Param        body body bulkRevokeRequest true "Token UUIDs to revoke"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/tokens/bulk [delete]
// @Security     BearerAuth
func AdminBulkRevokeTokens(db *database.DB) http.HandlerFunc {
	tokenSvc := services.NewTokenService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		adminID := middleware.GetUserID(r.Context())

		var req bulkRevokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if len(req.TokenUUIDs) == 0 {
			jsonError(w, "token_uuids is required", http.StatusBadRequest)
			return
		}

		// Resolve UUIDs to IDs
		var ids []int64
		for _, u := range req.TokenUUIDs {
			t, err := tokenSvc.GetByUUID(u)
			if err == nil {
				ids = append(ids, t.ID)
			}
		}

		revoked, err := tokenSvc.BulkRevoke(ids, adminID, req.Reason)
		if err != nil {
			jsonError(w, "failed to revoke tokens", http.StatusInternalServerError)
			return
		}

		auditEvent(r, logSvc, "api_token_bulk_revoked", "info", &adminID,
			fmt.Sprintf("count=%d reason=%s", revoked, req.Reason))

		jsonResponse(w, http.StatusOK, map[string]any{
			"message": "tokens revoked",
			"revoked": revoked,
		})
	}
}
