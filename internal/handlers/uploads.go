package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	antd "github.com/maidsafe/ant-sdk/antd-go"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/middleware"
	"github.com/maidsafe/indelible/internal/services"
	"github.com/maidsafe/indelible/internal/worker"
)

type uploadResponse struct {
	UUID             string  `json:"uuid"`
	Filename         string  `json:"filename"`
	OriginalFilename string  `json:"original_filename"`
	FileSize         int64   `json:"file_size"`
	ContentType      string  `json:"content_type"`
	Visibility       string  `json:"visibility"`
	Status           string  `json:"status"`
	DatamapAddress   *string `json:"datamap_address"`
	EstimatedCost    *string `json:"estimated_cost"`
	ActualCost       *string `json:"actual_cost"`
	ErrorMessage     *string `json:"error_message"`
	QueuedAt         string  `json:"queued_at"`
	ProcessingAt     *string `json:"processing_at"`
	CompletedAt      *string `json:"completed_at"`
	FailedAt         *string `json:"failed_at"`
	CreatedAt        string  `json:"created_at"`
}

func toUploadResponse(u *services.Upload) uploadResponse {
	r := uploadResponse{
		UUID:             u.UUID,
		Filename:         u.Filename,
		OriginalFilename: u.OriginalFilename,
		FileSize:         u.FileSize,
		ContentType:      u.ContentType,
		Visibility:       u.Visibility,
		Status:           u.Status,
		QueuedAt:         u.QueuedAt.Format("2006-01-02T15:04:05Z"),
		CreatedAt:        u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if u.DatamapAddress.Valid {
		r.DatamapAddress = &u.DatamapAddress.String
	}
	if u.EstimatedCost.Valid {
		r.EstimatedCost = &u.EstimatedCost.String
	}
	if u.ActualCost.Valid {
		r.ActualCost = &u.ActualCost.String
	}
	if u.ErrorMessage.Valid {
		r.ErrorMessage = &u.ErrorMessage.String
	}
	if u.ProcessingAt.Valid {
		s := u.ProcessingAt.Time.Format("2006-01-02T15:04:05Z")
		r.ProcessingAt = &s
	}
	if u.CompletedAt.Valid {
		s := u.CompletedAt.Time.Format("2006-01-02T15:04:05Z")
		r.CompletedAt = &s
	}
	if u.FailedAt.Valid {
		s := u.FailedAt.Time.Format("2006-01-02T15:04:05Z")
		r.FailedAt = &s
	}
	return r
}

// CreateUpload handles multipart file upload, saves to temp, and queues for processing.
func CreateUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	quotaSvc := services.NewQuotaService(db)
	maxUploadSize := int64(10 << 30) // 10 GB default

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		tokenID := middleware.GetTokenID(r.Context())

		// Limit request body size
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

		// Parse multipart form
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB memory buffer
			jsonError(w, "file too large or invalid multipart form", http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, "file field is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Read visibility from form field
		visibility := r.FormValue("visibility")
		if visibility == "" {
			visibility = "private"
		}
		if visibility != "public" && visibility != "private" {
			jsonError(w, "visibility must be 'public' or 'private'", http.StatusBadRequest)
			return
		}

		// Sanitize filename: strip path components and null bytes
		originalFilename := filepath.Base(header.Filename)
		originalFilename = strings.ReplaceAll(originalFilename, "\x00", "")
		if originalFilename == "" || originalFilename == "." {
			jsonError(w, "invalid filename", http.StatusBadRequest)
			return
		}

		// Generate a safe internal filename
		ext := filepath.Ext(originalFilename)
		safeFilename := uuid.New().String() + ext

		// Detect content type
		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		// Write to temp directory
		tempDir := worker.TempUploadDir(cfg)
		tempPath := filepath.Join(tempDir, safeFilename)

		dst, err := os.Create(tempPath)
		if err != nil {
			jsonError(w, "failed to create temp file", http.StatusInternalServerError)
			return
		}

		written, err := io.Copy(dst, file)
		dst.Close()
		if err != nil {
			os.Remove(tempPath)
			jsonError(w, "failed to save file", http.StatusInternalServerError)
			return
		}

		// Check quota before accepting upload
		if err := quotaSvc.CheckUserQuota(userID, written); err != nil {
			os.Remove(tempPath)
			jsonError(w, "quota exceeded: "+err.Error(), http.StatusForbidden)
			return
		}

		// Create upload record
		var tID *int64
		if tokenID != 0 {
			tID = &tokenID
		}

		upload, err := uploadSvc.Create(
			userID, tID,
			safeFilename, originalFilename,
			written, contentType, visibility,
			tempPath, nil,
		)
		if err != nil {
			os.Remove(tempPath)
			jsonError(w, "failed to queue upload", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusAccepted, map[string]any{
			"message": "upload queued",
			"upload":  toUploadResponse(upload),
		})
	}
}

// ListUploads returns the authenticated user's uploads.
func ListUploads(db *sql.DB) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

		uploads, total, err := uploadSvc.ListByUser(userID, limit, offset)
		if err != nil {
			jsonError(w, "failed to list uploads", http.StatusInternalServerError)
			return
		}

		resp := make([]uploadResponse, 0, len(uploads))
		for _, u := range uploads {
			resp = append(resp, toUploadResponse(u))
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"uploads": resp,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	}
}

// GetUpload returns a single upload by UUID.
func GetUpload(db *sql.DB) http.HandlerFunc {
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

		// Users can only see their own uploads
		if upload.UserID != userID {
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		jsonResponse(w, http.StatusOK, toUploadResponse(upload))
	}
}

// QuoteUpload estimates the cost of uploading a file.
// Accepts multipart with a "file" field — saves to temp, gets cost from antd, cleans up.
func QuoteUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	client := antd.NewClient(cfg.AntdURL)

	return func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")

		// JSON body path: estimate from file_size
		if strings.HasPrefix(contentType, "application/json") {
			var req struct {
				FileSize   int64  `json:"file_size"`
				Visibility string `json:"visibility"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if req.FileSize <= 0 {
				jsonError(w, "file_size must be positive", http.StatusBadRequest)
				return
			}
			if req.Visibility == "" {
				req.Visibility = "private"
			}

			// Use DataCost with a representative 1KB sample, then scale
			sampleSize := int64(1024)
			if req.FileSize < sampleSize {
				sampleSize = req.FileSize
			}
			sample := make([]byte, sampleSize)

			cost, err := client.DataCost(r.Context(), sample)
			if err != nil {
				jsonError(w, fmt.Sprintf("cost estimation failed: %v", err), http.StatusBadGateway)
				return
			}

			// Scale the cost estimate proportionally
			estimatedCost := scaleCost(cost, req.FileSize, sampleSize)

			jsonResponse(w, http.StatusOK, map[string]any{
				"estimated_cost": estimatedCost,
				"file_size":      req.FileSize,
				"visibility":     req.Visibility,
				"note":           "estimate based on current network pricing; actual cost may vary",
			})
			return
		}

		// Multipart path: save file, get exact cost, clean up
		r.Body = http.MaxBytesReader(w, r.Body, 10<<30)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			jsonError(w, "file too large or invalid form", http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, "file field required (or send JSON with file_size)", http.StatusBadRequest)
			return
		}
		defer file.Close()

		visibility := r.FormValue("visibility")
		if visibility == "" {
			visibility = "private"
		}

		// Write to temp
		tempDir := worker.TempUploadDir(cfg)
		tempPath := filepath.Join(tempDir, "quote-"+uuid.New().String())
		dst, err := os.Create(tempPath)
		if err != nil {
			jsonError(w, "failed to create temp file", http.StatusInternalServerError)
			return
		}
		written, err := io.Copy(dst, file)
		dst.Close()
		if err != nil {
			os.Remove(tempPath)
			jsonError(w, "failed to save file", http.StatusInternalServerError)
			return
		}
		defer os.Remove(tempPath)

		// Get cost from antd
		isPublic := visibility == "public"
		cost, err := client.FileCost(r.Context(), tempPath, isPublic, true)
		if err != nil {
			jsonError(w, fmt.Sprintf("cost estimation failed: %v", err), http.StatusBadGateway)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"estimated_cost":    cost,
			"file_size":         written,
			"original_filename": filepath.Base(header.Filename),
			"visibility":        visibility,
		})
	}
}

// DownloadUpload retrieves a completed upload's data from the Autonomi network.
func DownloadUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	client := antd.NewClient(cfg.AntdURL)

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

		// Users can only download their own uploads (unless public — future: allow public access)
		if upload.UserID != userID {
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		if upload.Status != "completed" {
			jsonError(w, fmt.Sprintf("upload is %s, not ready for download", upload.Status), http.StatusConflict)
			return
		}

		if !upload.DatamapAddress.Valid {
			jsonError(w, "upload has no network address", http.StatusInternalServerError)
			return
		}

		// Download from antd to temp file, then stream to client
		tempDir := worker.TempUploadDir(cfg)
		tempPath := filepath.Join(tempDir, "dl-"+uuid.New().String())
		defer os.Remove(tempPath)

		if upload.Visibility == "public" {
			if err := client.FileDownloadPublic(r.Context(), upload.DatamapAddress.String, tempPath); err != nil {
				jsonError(w, fmt.Sprintf("download from network failed: %v", err), http.StatusBadGateway)
				return
			}
		} else {
			// Private: get bytes via DataGetPrivate, write to temp
			data, err := client.DataGetPrivate(r.Context(), upload.DatamapAddress.String)
			if err != nil {
				jsonError(w, fmt.Sprintf("download from network failed: %v", err), http.StatusBadGateway)
				return
			}
			if err := os.WriteFile(tempPath, data, 0600); err != nil {
				jsonError(w, "failed to write download", http.StatusInternalServerError)
				return
			}
		}

		// Stream the file back
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, upload.OriginalFilename))
		w.Header().Set("Content-Type", upload.ContentType)
		http.ServeFile(w, r, tempPath)
	}
}

// scaleCost provides a rough linear cost estimate.
// cost is the atto-token cost string for sampleSize bytes; we scale to targetSize.
func scaleCost(cost string, targetSize, sampleSize int64) string {
	if sampleSize <= 0 || targetSize <= 0 {
		return cost
	}
	// Parse cost as integer (atto tokens)
	// For simplicity, we do integer math to avoid floating point
	costVal := int64(0)
	fmt.Sscanf(cost, "%d", &costVal)
	if costVal == 0 {
		return cost
	}
	scaled := costVal * targetSize / sampleSize
	return fmt.Sprintf("%d", scaled)
}
