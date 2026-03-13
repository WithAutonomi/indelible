package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/maidsafe/indelible/internal/services"
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

func AdminListQuotas(db *sql.DB) http.HandlerFunc {
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

func AdminCreateQuota(db *sql.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)

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
			if errors.Is(err, services.ErrQuotaDuplicate) {
				jsonError(w, "quota already exists for this entity", http.StatusConflict)
				return
			}
			jsonError(w, "failed to create quota", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, toQuotaResponse(quota))
	}
}

func AdminUpdateQuota(db *sql.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)

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

		jsonResponse(w, http.StatusOK, toQuotaResponse(quota))
	}
}

func AdminDeleteQuota(db *sql.DB) http.HandlerFunc {
	quotaSvc := services.NewQuotaService(db)

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

		jsonResponse(w, http.StatusOK, map[string]string{"message": "quota deleted"})
	}
}
