package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/maidsafe/indelible/internal/middleware"
	"github.com/maidsafe/indelible/internal/services"
)

// UpdateTags sets tags on an upload (replace-all semantics).
func UpdateTags(db *sql.DB) http.HandlerFunc {
	tagSvc := services.NewTagService(db)
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		uploadUUID := chi.URLParam(r, "id")

		// Verify ownership
		upload, err := uploadSvc.GetByUUID(uploadUUID)
		if err != nil {
			if errors.Is(err, services.ErrUploadNotFound) {
				jsonError(w, "upload not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to get upload", http.StatusInternalServerError)
			return
		}
		if upload.UserID != userID {
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		var req struct {
			Tags map[string]string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Tags == nil {
			req.Tags = make(map[string]string)
		}

		if err := tagSvc.SetTags(upload.ID, req.Tags); err != nil {
			jsonError(w, "failed to update tags", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"message": "tags updated",
			"tags":    req.Tags,
		})
	}
}

// SearchByTags searches uploads by tag filters and/or filename query.
func SearchByTags(db *sql.DB) http.HandlerFunc {
	tagSvc := services.NewTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		query := r.URL.Query().Get("q")

		// Parse tag filters from query params: tag.key=value
		tagFilters := make(map[string]string)
		for k, vals := range r.URL.Query() {
			if len(k) > 4 && k[:4] == "tag." {
				tagFilters[k[4:]] = vals[0]
			}
		}

		results, total, err := tagSvc.Search(tagFilters, query, userID, limit, offset)
		if err != nil {
			jsonError(w, "search failed", http.StatusInternalServerError)
			return
		}

		type searchResultResponse struct {
			Upload uploadResponse    `json:"upload"`
			Tags   map[string]string `json:"tags"`
		}

		resp := make([]searchResultResponse, 0, len(results))
		for _, r := range results {
			resp = append(resp, searchResultResponse{
				Upload: toUploadResponse(r.Upload),
				Tags:   r.Tags,
			})
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"results": resp,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	}
}
