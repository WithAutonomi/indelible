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

type collectionResponse struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ParentID    *int64  `json:"parent_id"`
	FileCount   int64   `json:"file_count"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
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

// CreateCollection creates a new collection/folder.
func CreateCollection(db *sql.DB) http.HandlerFunc {
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

// ListCollections returns the user's collections. Use ?parent_id=N to list children.
func ListCollections(db *sql.DB) http.HandlerFunc {
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

// GetCollection returns a single collection with its file count.
func GetCollection(db *sql.DB) http.HandlerFunc {
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

		// Include files
		files, total, _ := collSvc.ListFiles(id, 50, 0)
		fileResp := make([]uploadResponse, 0, len(files))
		for _, f := range files {
			fileResp = append(fileResp, toUploadResponse(f))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"collection":  toCollectionResponse(coll),
			"files":       fileResp,
			"total_files": total,
		})
	}
}

// UpdateCollection modifies a collection's name and description.
func UpdateCollection(db *sql.DB) http.HandlerFunc {
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
func DeleteCollection(db *sql.DB) http.HandlerFunc {
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
func AddToCollection(db *sql.DB) http.HandlerFunc {
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

		jsonResponse(w, http.StatusOK, map[string]string{"message": "file added to collection"})
	}
}

// RemoveFromCollection removes an upload from a collection.
func RemoveFromCollection(db *sql.DB) http.HandlerFunc {
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

		jsonResponse(w, http.StatusOK, map[string]string{"message": "file removed from collection"})
	}
}
