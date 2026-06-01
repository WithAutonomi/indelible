package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type quotaResponse struct {
	ID         int64   `json:"id"`
	EntityType string  `json:"entity_type"`
	EntityID   *string `json:"entity_id"`
	MaxBytes   int64   `json:"max_bytes"`
	UsedBytes  int64   `json:"used_bytes"`
	IsEnabled  bool    `json:"is_enabled"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func toQuotaResponse(q *services.Quota) quotaResponse {
	r := quotaResponse{
		ID:         q.ID,
		EntityType: q.EntityType,
		MaxBytes:   q.MaxBytes,
		UsedBytes:  q.UsedBytes,
		IsEnabled:  q.IsEnabled,
		CreatedAt:  q.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:  q.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if q.EntityID.Valid {
		r.EntityID = &q.EntityID.String
	}
	return r
}

type createQuotaRequest struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	MaxBytes   int64  `json:"max_bytes"`
}

type updateQuotaRequest struct {
	MaxBytes  int64 `json:"max_bytes"`
	IsEnabled bool  `json:"is_enabled"`
}

// @Summary      List all quotas
// @Description  Return all configured storage quotas
// @Tags         Admin: Quotas
// @Produce      json
// @Success      200 {object} map[string][]quotaResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/quotas [get]
// @Security     BearerAuth
func AdminListQuotas(db *database.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		quotas, err := quotaSvc.List()
		if err != nil {
			jsonError(w, "failed to list quotas", http.StatusInternalServerError)
			return
		}

		resp := make([]quotaResponse, 0, len(quotas))
		for _, q := range quotas {
			resp = append(resp, toQuotaResponse(q))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"quotas": resp})
	}
}

// @Summary      Create a quota
// @Description  Create a new storage quota for an entity type
// @Tags         Admin: Quotas
// @Accept       json
// @Produce      json
// @Param        body body createQuotaRequest true "Quota details"
// @Success      201 {object} quotaResponse
// @Failure      400 {object} map[string]string
// @Failure      409 {object} map[string]string "Duplicate quota"
// @Failure      500 {object} map[string]string
// @Router       /admin/quotas [post]
// @Security     BearerAuth
func AdminCreateQuota(db *database.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createQuotaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.EntityType == "" {
			jsonError(w, "entity_type is required", http.StatusBadRequest)
			return
		}
		if req.MaxBytes <= 0 {
			jsonError(w, "max_bytes must be positive", http.StatusBadRequest)
			return
		}

		quota, err := quotaSvc.Create(req.EntityType, req.EntityID, req.MaxBytes)
		if err != nil {
			switch {
			case errors.Is(err, services.ErrQuotaDuplicate):
				jsonError(w, "quota already exists for this entity", http.StatusConflict)
			case errors.Is(err, services.ErrQuotaEntityRequired),
				errors.Is(err, services.ErrQuotaEntityNotFound),
				errors.Is(err, services.ErrQuotaInvalidEntityType):
				jsonError(w, err.Error(), http.StatusBadRequest)
			default:
				jsonError(w, "failed to create quota", http.StatusInternalServerError)
			}
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "quota_created", "info", &callerID,
			fmt.Sprintf("id=%d entity_type=%s entity_id=%s max_bytes=%d", quota.ID, req.EntityType, req.EntityID, req.MaxBytes))

		jsonResponse(w, http.StatusCreated, toQuotaResponse(quota))
	}
}

// @Summary      List known departments
// @Description  Return the distinct department labels in use across API tokens, for the quota dialog's department picker
// @Tags         Admin: Quotas
// @Produce      json
// @Success      200 {object} map[string][]string
// @Failure      500 {object} map[string]string
// @Router       /admin/departments [get]
// @Security     BearerAuth
func AdminListDepartments(db *database.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		depts, err := quotaSvc.Departments()
		if err != nil {
			jsonError(w, "failed to list departments", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"departments": depts})
	}
}

// @Summary      Update a quota
// @Description  Update a quota's max bytes and enabled status
// @Tags         Admin: Quotas
// @Accept       json
// @Produce      json
// @Param        id   path int                true "Quota ID"
// @Param        body body updateQuotaRequest true "Updated quota fields"
// @Success      200 {object} quotaResponse
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/quotas/{id} [put]
// @Security     BearerAuth
func AdminUpdateQuota(db *database.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid quota id", http.StatusBadRequest)
			return
		}

		var req updateQuotaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		quota, err := quotaSvc.Update(id, req.MaxBytes, req.IsEnabled)
		if err != nil {
			if errors.Is(err, services.ErrQuotaNotFound) {
				jsonError(w, "quota not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to update quota", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "quota_updated", "info", &callerID,
			fmt.Sprintf("id=%d max_bytes=%d enabled=%t", id, req.MaxBytes, req.IsEnabled))

		jsonResponse(w, http.StatusOK, toQuotaResponse(quota))
	}
}

// @Summary      Delete a quota
// @Description  Remove a storage quota
// @Tags         Admin: Quotas
// @Produce      json
// @Param        id path int true "Quota ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/quotas/{id} [delete]
// @Security     BearerAuth
func AdminDeleteQuota(db *database.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid quota id", http.StatusBadRequest)
			return
		}

		if err := quotaSvc.Delete(id); err != nil {
			if errors.Is(err, services.ErrQuotaNotFound) {
				jsonError(w, "quota not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to delete quota", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "quota_deleted", "info", &callerID, fmt.Sprintf("id=%d", id))

		jsonResponse(w, http.StatusOK, map[string]string{"message": "quota deleted"})
	}
}
