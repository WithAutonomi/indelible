package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// ListTagKeys returns all distinct tag keys used by the current user.
func ListTagKeys(db *sql.DB) http.HandlerFunc {
	tagSvc := services.NewTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		keys, err := tagSvc.ListKeys(userID)
		if err != nil {
			jsonError(w, "failed to list tag keys", http.StatusInternalServerError)
			return
		}
		if keys == nil {
			keys = []string{}
		}
		jsonResponse(w, http.StatusOK, map[string]any{"keys": keys})
	}
}

// ListTagValues returns all distinct values for a given tag key used by the current user.
func ListTagValues(db *sql.DB) http.HandlerFunc {
	tagSvc := services.NewTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		key := r.URL.Query().Get("key")
		if key == "" {
			jsonError(w, "key query parameter is required", http.StatusBadRequest)
			return
		}
		values, err := tagSvc.ListValues(userID, key)
		if err != nil {
			jsonError(w, "failed to list tag values", http.StatusInternalServerError)
			return
		}
		if values == nil {
			values = []string{}
		}
		jsonResponse(w, http.StatusOK, map[string]any{"values": values})
	}
}

// GetTags godoc
// @Summary Get tags for an upload
// @Description Returns all tags on an upload as a key-value map
// @Tags Tags
// @Produce json
// @Param id path string true "Upload UUID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /uploads/{id}/tags [get]
// @Security BearerAuth
func GetTags(db *sql.DB) http.HandlerFunc {
	tagSvc := services.NewTagService(db)
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		uploadUUID := chi.URLParam(r, "id")

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

		tags, err := tagSvc.GetTags(upload.ID)
		if err != nil {
			jsonError(w, "failed to get tags", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"tags": tags,
		})
	}
}

// UpdateTags godoc
// @Summary Update tags on an upload
// @Description Replace all tags on an upload with the provided set
// @Tags Tags
// @Accept json
// @Produce json
// @Param id path string true "Upload UUID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /uploads/{id}/tags [put]
// @Security BearerAuth
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

		webhookSvc := services.NewWebhookDeliveryService(db)
		go webhookSvc.FireTagEvent("tags_updated", upload.UUID, req.Tags)

		jsonResponse(w, http.StatusOK, map[string]any{
			"message": "tags updated",
			"tags":    req.Tags,
		})
	}
}

// SearchByTags godoc
// @Summary Search uploads by tags
// @Description Search uploads by tag key-value filters and/or filename query
// @Tags Tags
// @Produce json
// @Param q query string false "Filename search query"
// @Param tag query string false "Tag filter (key=value)"
// @Success 200 {object} map[string]interface{}
// @Router /search [get]
// @Security BearerAuth
func SearchByTags(db *sql.DB) http.HandlerFunc {
	tagSvc := services.NewTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		query := r.URL.Query().Get("q")
		selector := r.URL.Query().Get("selector")

		var results []*services.SearchResult
		var total int64
		var err error

		if selector != "" {
			// New: label selector syntax (e.g., "department=engineering,status!=archived")
			reqs, parseErr := services.ParseSelector(selector)
			if parseErr != nil {
				jsonError(w, "invalid selector: "+parseErr.Error(), http.StatusBadRequest)
				return
			}
			clauses, args := services.BuildSelectorSQL(reqs)
			results, total, err = tagSvc.SearchWithSelector(clauses, args, query, userID, limit, offset)
		} else {
			// Legacy: tag.key=value query params
			tagFilters := make(map[string]string)
			for k, vals := range r.URL.Query() {
				if len(k) > 4 && k[:4] == "tag." {
					tagFilters[k[4:]] = vals[0]
				}
			}
			results, total, err = tagSvc.Search(tagFilters, query, userID, limit, offset)
		}

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
