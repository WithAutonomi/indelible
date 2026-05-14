package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// GetCollectionTags returns tags on a collection.
func GetCollectionTags(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)
	collTagSvc := services.NewCollectionTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection ID", http.StatusBadRequest)
			return
		}

		coll, err := collSvc.GetByID(id)
		if err != nil {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}
		if coll.CreatedBy != userID {
			jsonError(w, "not your collection", http.StatusForbidden)
			return
		}

		tags, err := collTagSvc.GetTags(id)
		if err != nil {
			jsonError(w, "failed to get tags", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{"tags": tags})
	}
}

// UpdateCollectionTags sets tags on a collection.
func UpdateCollectionTags(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)
	collTagSvc := services.NewCollectionTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection ID", http.StatusBadRequest)
			return
		}

		coll, err := collSvc.GetByID(id)
		if err != nil {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}
		if coll.CreatedBy != userID {
			jsonError(w, "not your collection", http.StatusForbidden)
			return
		}

		var req struct {
			Tags map[string][]string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := collTagSvc.SetTags(id, req.Tags); err != nil {
			jsonError(w, "failed to update tags", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{"message": "collection tags updated", "tags": req.Tags})
	}
}
