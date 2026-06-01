package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type collectionResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id"`
	FileCount   int64  `json:"file_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toCollectionResponse(c *services.Collection) collectionResponse {
	r := collectionResponse{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		FileCount:   c.FileCount,
		CreatedAt:   c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if c.ParentID.Valid {
		r.ParentID = &c.ParentID.Int64
	}
	return r
}

type createCollectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id"`
}

type updateCollectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type addFileRequest struct {
	UploadUUID string `json:"upload_uuid"`
}

// CreateCollection godoc
// @Summary Create a new collection
// @Tags Collections
// @Accept json
// @Produce json
// @Param body body createCollectionRequest true "Collection details"
// @Success 201 {object} collectionResponse
// @Failure 400 {object} map[string]string
// @Router /collections [post]
// @Security BearerAuth
func CreateCollection(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var req createCollectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}

		coll, err := collSvc.Create(req.Name, req.Description, req.ParentID, userID)
		if err != nil {
			if errors.Is(err, services.ErrCollectionNotFound) {
				jsonError(w, "parent collection not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to create collection", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, toCollectionResponse(coll))
	}
}

// ListCollections godoc
// @Summary List collections
// @Tags Collections
// @Produce json
// @Param parent_id query int false "Parent collection ID"
// @Success 200 {object} map[string]interface{}
// @Router /collections [get]
// @Security BearerAuth
func ListCollections(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var parentID *int64
		if pidStr := r.URL.Query().Get("parent_id"); pidStr != "" {
			pid, err := strconv.ParseInt(pidStr, 10, 64)
			if err == nil {
				parentID = &pid
			}
		}

		collections, err := collSvc.List(userID, parentID)
		if err != nil {
			jsonError(w, "failed to list collections", http.StatusInternalServerError)
			return
		}

		resp := make([]collectionResponse, 0, len(collections))
		for _, c := range collections {
			resp = append(resp, toCollectionResponse(c))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"collections": resp})
	}
}

// GetCollection godoc
// @Summary Get collection by ID
// @Tags Collections
// @Produce json
// @Param id path int true "Collection ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /collections/{id} [get]
// @Security BearerAuth
func GetCollection(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection id", http.StatusBadRequest)
			return
		}

		coll, err := collSvc.GetByID(id)
		if err != nil {
			if errors.Is(err, services.ErrCollectionNotFound) {
				jsonError(w, "collection not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to get collection", http.StatusInternalServerError)
			return
		}

		// Users can only see their own collections
		if coll.CreatedBy != userID {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}

		// Include files — paginated; the frontend drives limit/offset. ListFiles
		// clamps limit to 1..100 (0 -> default 50), so a missing limit is safe.
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		files, total, _ := collSvc.ListFiles(id, limit, offset)
		addedTimes, _ := collSvc.AddedTimes(id)

		// Carry the membership added_at alongside the upload fields so the UI's
		// "Added" column isn't "Invalid Date".
		type collectionFileResponse struct {
			uploadResponse
			AddedAt string `json:"added_at"`
		}
		fileResp := make([]collectionFileResponse, 0, len(files))
		for _, f := range files {
			cfr := collectionFileResponse{uploadResponse: toUploadResponse(f)}
			if t, ok := addedTimes[f.ID]; ok {
				cfr.AddedAt = t.Format("2006-01-02T15:04:05Z")
			}
			fileResp = append(fileResp, cfr)
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"collection":  toCollectionResponse(coll),
			"files":       fileResp,
			"total_files": total,
		})
	}
}

// UpdateCollection modifies a collection's name and description.
// UpdateCollection godoc
// @Summary Update a collection
// @Tags Collections
// @Accept json
// @Produce json
// @Param id path int true "Collection ID"
// @Success 200 {object} collectionResponse
// @Failure 404 {object} map[string]string
// @Router /collections/{id} [put]
// @Security BearerAuth
func UpdateCollection(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection id", http.StatusBadRequest)
			return
		}

		// Verify ownership
		coll, err := collSvc.GetByID(id)
		if err != nil || coll.CreatedBy != userID {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}

		var req updateCollectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}

		updated, err := collSvc.Update(id, req.Name, req.Description)
		if err != nil {
			jsonError(w, "failed to update collection", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, toCollectionResponse(updated))
	}
}

// DeleteCollection removes a collection and its children (files are not deleted).
// DeleteCollection godoc
// @Summary Delete a collection
// @Tags Collections
// @Produce json
// @Param id path int true "Collection ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /collections/{id} [delete]
// @Security BearerAuth
func DeleteCollection(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection id", http.StatusBadRequest)
			return
		}

		// Verify ownership
		coll, err := collSvc.GetByID(id)
		if err != nil || coll.CreatedBy != userID {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}

		if err := collSvc.Delete(id); err != nil {
			jsonError(w, "failed to delete collection", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "collection deleted"})
	}
}

// AddToCollection adds an upload to a collection.
// AddToCollection godoc
// @Summary Add file to collection
// @Tags Collections
// @Accept json
// @Produce json
// @Param id path int true "Collection ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /collections/{id}/files [post]
// @Security BearerAuth
func AddToCollection(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)
	collTagSvc := services.NewCollectionTagService(db)
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		collID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection id", http.StatusBadRequest)
			return
		}

		// Verify collection ownership
		coll, err := collSvc.GetByID(collID)
		if err != nil || coll.CreatedBy != userID {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}

		var req addFileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Resolve upload UUID and verify ownership
		upload, err := uploadSvc.GetByUUID(req.UploadUUID)
		if err != nil || upload.UserID != userID {
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		if err := collSvc.AddFile(collID, upload.ID); err != nil {
			if errors.Is(err, services.ErrFileAlreadyInCollection) {
				jsonError(w, "file already in collection", http.StatusConflict)
				return
			}
			jsonError(w, "failed to add file", http.StatusInternalServerError)
			return
		}

		// Inherit collection tags to the file (additive, won't overwrite existing)
		_, _ = collTagSvc.InheritToFile(collID, upload.ID)

		webhookSvc := services.NewWebhookDeliveryService(db)
		go webhookSvc.FireCollectionEvent("collection_file_added", upload.UUID, collID, coll.Name)

		jsonResponse(w, http.StatusOK, map[string]string{"message": "file added to collection"})
	}
}

// RemoveFromCollection removes an upload from a collection.
// RemoveFromCollection godoc
// @Summary Remove file from collection
// @Tags Collections
// @Produce json
// @Param id path int true "Collection ID"
// @Param upload_id path string true "Upload UUID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /collections/{id}/files/{upload_id} [delete]
// @Security BearerAuth
func RemoveFromCollection(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		collID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid collection id", http.StatusBadRequest)
			return
		}

		// Verify collection ownership
		coll, err := collSvc.GetByID(collID)
		if err != nil || coll.CreatedBy != userID {
			jsonError(w, "collection not found", http.StatusNotFound)
			return
		}

		uploadUUID := chi.URLParam(r, "uploadId")
		upload, err := uploadSvc.GetByUUID(uploadUUID)
		if err != nil {
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		if err := collSvc.RemoveFile(collID, upload.ID); err != nil {
			if errors.Is(err, services.ErrFileNotInCollection) {
				jsonError(w, "file not in collection", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to remove file", http.StatusInternalServerError)
			return
		}

		webhookSvc := services.NewWebhookDeliveryService(db)
		go webhookSvc.FireCollectionEvent("collection_file_removed", upload.UUID, collID, coll.Name)

		jsonResponse(w, http.StatusOK, map[string]string{"message": "file removed from collection"})
	}
}

// UploadCollections returns the collection IDs that contain a given upload.
// This allows the frontend to fetch membership in a single request instead of N+1.
//
// @Summary Get collections containing an upload
// @Tags Collections
// @Produce json
// @Param id path string true "Upload UUID"
// @Success 200 {object} map[string]any
// @Router /uploads/{id}/collections [get]
// @Security BearerAuth
func UploadCollections(db *database.DB) http.HandlerFunc {
	collSvc := services.NewCollectionService(db)
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		uploadUUID := chi.URLParam(r, "id")

		upload, err := uploadSvc.GetByUUID(uploadUUID)
		if err != nil || upload.UserID != userID {
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		ids, err := collSvc.CollectionIDsForUpload(upload.ID)
		if err != nil {
			jsonError(w, "failed to get collections", http.StatusInternalServerError)
			return
		}
		if ids == nil {
			ids = []int64{}
		}

		jsonResponse(w, http.StatusOK, map[string]any{"collection_ids": ids})
	}
}
