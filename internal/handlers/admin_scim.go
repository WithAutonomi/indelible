package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type scimTokenResponse struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	IsActive   bool    `json:"is_active"`
	CreatedBy  int64   `json:"created_by"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func toScimTokenResponse(t *services.ScimToken) scimTokenResponse {
	resp := scimTokenResponse{
		ID:        t.ID,
		Name:      t.Name,
		IsActive:  t.IsActive,
		CreatedBy: t.CreatedBy,
		CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if t.LastUsedAt.Valid {
		s := t.LastUsedAt.Time.Format("2006-01-02T15:04:05Z")
		resp.LastUsedAt = &s
	}
	if t.RevokedAt.Valid {
		s := t.RevokedAt.Time.Format("2006-01-02T15:04:05Z")
		resp.RevokedAt = &s
	}
	return resp
}

// @Summary      Create a SCIM token
// @Description  Generate a new SCIM bearer token for identity provider integration
// @Tags         Admin: SCIM
// @Accept       json
// @Produce      json
// @Param        body body object{name=string} true "Token name"
// @Success      201 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/scim/tokens [post]
// @Security     BearerAuth
func AdminCreateScimToken(db *sql.DB) http.HandlerFunc {
	scimTokenSvc := services.NewScimTokenService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}

		userID := middleware.GetUserID(r.Context())
		secret, token, err := scimTokenSvc.Create(req.Name, userID)
		if err != nil {
			jsonError(w, "failed to create SCIM token", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, map[string]any{
			"token":  toScimTokenResponse(token),
			"secret": secret,
		})
	}
}

// @Summary      List SCIM tokens
// @Description  Return all SCIM tokens (no secrets shown)
// @Tags         Admin: SCIM
// @Produce      json
// @Success      200 {object} map[string][]scimTokenResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/scim/tokens [get]
// @Security     BearerAuth
func AdminListScimTokens(db *sql.DB) http.HandlerFunc {
	scimTokenSvc := services.NewScimTokenService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		tokens, err := scimTokenSvc.List()
		if err != nil {
			jsonError(w, "failed to list SCIM tokens", http.StatusInternalServerError)
			return
		}

		resp := make([]scimTokenResponse, 0, len(tokens))
		for _, t := range tokens {
			resp = append(resp, toScimTokenResponse(t))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"tokens": resp})
	}
}

// @Summary      Revoke a SCIM token
// @Description  Revoke a SCIM token by ID
// @Tags         Admin: SCIM
// @Produce      json
// @Param        id path int true "Token ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/scim/tokens/{id} [delete]
// @Security     BearerAuth
func AdminRevokeScimToken(db *sql.DB) http.HandlerFunc {
	scimTokenSvc := services.NewScimTokenService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid token id", http.StatusBadRequest)
			return
		}

		if err := scimTokenSvc.Revoke(id); err != nil {
			if err == services.ErrScimTokenNotFound {
				jsonError(w, "SCIM token not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to revoke SCIM token", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "SCIM token revoked"})
	}
}
