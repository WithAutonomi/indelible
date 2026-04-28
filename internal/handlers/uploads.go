package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
	"github.com/WithAutonomi/indelible/internal/worker"
)

type uploadResponse struct {
	UUID             string  `json:"uuid"`
	Filename         string  `json:"filename"`
	OriginalFilename string  `json:"original_filename"`
	FileSize         int64   `json:"file_size"`
	ContentType      string  `json:"content_type"`
	Visibility       string  `json:"visibility"`
	Status           string  `json:"status"`
	StatusDetail     *string `json:"status_detail,omitempty"`
	DatamapAddress   *string `json:"datamap_address"`
	EstimatedCost    *string `json:"estimated_cost"`
	ActualCost       *string `json:"actual_cost"`
	ErrorMessage     *string `json:"error_message"`
	BackoffUntil     *string `json:"backoff_until,omitempty"`
	BackoffAttempt   int     `json:"backoff_attempt,omitempty"`
	LastQuotedCost   *string `json:"last_quoted_cost,omitempty"`
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
	if u.StatusDetail.Valid {
		r.StatusDetail = &u.StatusDetail.String
	}
	if u.BackoffUntil.Valid {
		s := u.BackoffUntil.Time.Format("2006-01-02T15:04:05Z")
		r.BackoffUntil = &s
	}
	r.BackoffAttempt = u.BackoffAttempt
	if u.LastQuotedCost.Valid {
		r.LastQuotedCost = &u.LastQuotedCost.String
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
//
// @Summary      Upload file
// @Description  Upload a file via multipart form data and queue it for processing
// @Tags         Uploads
// @Accept       multipart/form-data
// @Produce      json
// @Param        file        formData  file    true   "File to upload"
// @Param        visibility  formData  string  false  "public or private"
// @Success      202  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Failure      503  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads [post]
func CreateUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	tagSvc := services.NewTagService(db)
	tagRuleSvc := services.NewTagRuleService(db)
	quotaSvc := services.NewQuotaService(db)
	webhookSvc := services.NewWebhookDeliveryService(db)
	settingsSvc := services.NewSettingsService(db)

	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)

	return func(w http.ResponseWriter, r *http.Request) {
		// Pre-flight: reject early if no wallet is configured
		wallet, err := walletSvc.GetDefault()
		if err != nil || wallet == nil {
			jsonErrorWithCode(w, "No wallet configured", "wallet_not_configured", http.StatusServiceUnavailable)
			return
		}

		userID := middleware.GetUserID(r.Context())
		tokenID := middleware.GetTokenID(r.Context())

		// S1: Check queue depth limit
		if maxQueuedStr, err := settingsSvc.Get("max_queued_uploads"); err == nil {
			if maxQueued, err := strconv.ParseInt(maxQueuedStr, 10, 64); err == nil && maxQueued > 0 {
				counts, _ := uploadSvc.CountByStatus()
				inFlight := counts["queued"] + counts["processing"]
				if inFlight >= maxQueued {
					w.Header().Set("Retry-After", "30")
					jsonErrorWithCode(w, "Upload queue is full, please try again later", "queue_full", http.StatusTooManyRequests)
					return
				}
			}
		}

		// S14: Read max_upload_size_bytes from settings
		maxUploadSize := int64(10 << 30) // 10 GB fallback
		if maxStr, err := settingsSvc.Get("max_upload_size_bytes"); err == nil {
			if n, err := strconv.ParseInt(maxStr, 10, 64); err == nil && n > 0 {
				maxUploadSize = n
			}
		}

		// Limit request body size
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

		// Parse multipart form
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB memory buffer
			jsonErrorWithCode(w, "file too large or invalid multipart form", "file_too_large", http.StatusBadRequest)
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

		// Parse tags from form data (JSON object: key -> string or string[])
		uploadTags := make(map[string][]string)
		if tagsJSON := r.FormValue("tags"); tagsJSON != "" {
			// Try multi-value format first: {"key": ["v1","v2"]}
			if err := json.Unmarshal([]byte(tagsJSON), &uploadTags); err != nil {
				// Fall back to single-value format: {"key": "value"}
				var singleTags map[string]string
				if err2 := json.Unmarshal([]byte(tagsJSON), &singleTags); err2 != nil {
					jsonError(w, "tags must be a JSON object of key to string or string array", http.StatusBadRequest)
					return
				}
				for k, v := range singleTags {
					uploadTags[k] = []string{v}
				}
			}
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

		// Detect and validate content type
		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if !isAllowedContentType(contentType, settingsSvc) {
			jsonError(w, "file type not allowed: "+contentType, http.StatusBadRequest)
			return
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
			jsonErrorWithCode(w, "quota exceeded: "+err.Error(), "quota_exceeded", http.StatusForbidden)
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

		// Apply auto-tag rules based on file attributes
		autoTags, err := tagRuleSvc.EvaluateRules(originalFilename, contentType, written, visibility)
		if err != nil {
			slog.Warn("auto-tag rule evaluation failed", "error", err)
		}

		// Merge: user-supplied tags take precedence over auto-generated
		for k, v := range autoTags {
			if _, exists := uploadTags[k]; !exists {
				uploadTags[k] = v
			}
		}

		// Set tags on the upload (if any)
		if len(uploadTags) > 0 {
			if err := tagSvc.SetTags(upload.ID, uploadTags); err != nil {
				slog.Warn("failed to set upload tags", "upload_id", upload.ID, "error", err)
			}
		}

		webhookSvc.FireUploadEvent("queued", upload)

		// S10: Include queue position for backpressure signaling
		queuePos := int64(0)
		if counts, err := uploadSvc.CountByStatus(); err == nil {
			queuePos = counts["queued"]
		}

		jsonResponse(w, http.StatusAccepted, map[string]any{
			"message":        "upload queued",
			"upload":         toUploadResponse(upload),
			"queue_position": queuePos,
		})
	}
}

// ListUploads returns the authenticated user's uploads.
//
// @Summary      List user's uploads
// @Description  List all uploads belonging to the authenticated user
// @Tags         Uploads
// @Produce      json
// @Param        limit   query  int  false  "Limit"
// @Param        offset  query  int  false  "Offset"
// @Success      200  {object}  map[string]any
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads [get]
func ListUploads(db *sql.DB) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))

		// Cursor-based pagination
		if cursorParam := q.Get("cursor"); cursorParam != "" {
			if limit <= 0 {
				limit = 50
			}
			cursor, err := DecodeCursor(cursorParam)
			if err != nil {
				jsonErrorWithCode(w, "invalid cursor", "validation_error", http.StatusBadRequest)
				return
			}
			forward := cursor.Dir != "prev"
			uploads, err := uploadSvc.ListByUserCursor(userID, limit+1, cursor.ID, forward)
			if err != nil {
				jsonError(w, "failed to list uploads", http.StatusInternalServerError)
				return
			}

			hasMore := len(uploads) > limit
			if hasMore {
				uploads = uploads[:limit]
			}

			resp := make([]uploadResponse, 0, len(uploads))
			for _, u := range uploads {
				resp = append(resp, toUploadResponse(u))
			}

			result := map[string]any{"uploads": resp}
			if hasMore && len(uploads) > 0 {
				result["next_cursor"] = EncodeCursor(Cursor{ID: uploads[len(uploads)-1].ID, Dir: "next"})
			}
			if len(uploads) > 0 {
				result["prev_cursor"] = EncodeCursor(Cursor{ID: uploads[0].ID, Dir: "prev"})
			}
			jsonResponse(w, http.StatusOK, result)
			return
		}

		// Sort/filter params
		sortParam := q.Get("sort")
		status := q.Get("status")
		fromParam := q.Get("from")
		toParam := q.Get("to")

		if sortParam != "" || status != "" || fromParam != "" || toParam != "" {
			opts := services.UploadListOptions{
				UserID: userID,
				Limit:  limit,
				Offset: offset,
				Status: status,
			}

			// Parse sort param: "field:direction" or just "field"
			if sortParam != "" {
				parts := strings.SplitN(sortParam, ":", 2)
				opts.SortBy = parts[0]
				if len(parts) > 1 {
					opts.SortOrder = parts[1]
				}
			}

			if fromParam != "" {
				if t, err := time.Parse(time.RFC3339, fromParam); err == nil {
					opts.From = &t
				}
			}
			if toParam != "" {
				if t, err := time.Parse(time.RFC3339, toParam); err == nil {
					opts.To = &t
				}
			}

			uploads, total, err := uploadSvc.ListByUserFiltered(opts)
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
				"limit":   opts.Limit,
				"offset":  opts.Offset,
			})
			return
		}

		// Default: simple offset/limit
		uploads, total, err := uploadSvc.ListByUser(userID, limit, offset)
		if err != nil {
			jsonError(w, "failed to list uploads", http.StatusInternalServerError)
			return
		}

		resp := make([]uploadResponse, 0, len(uploads))
		for _, u := range uploads {
			resp = append(resp, toUploadResponse(u))
		}

		// Include cursor hints in default listing too
		result := map[string]any{
			"uploads": resp,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		}
		if len(uploads) > 0 {
			result["next_cursor"] = EncodeCursor(Cursor{ID: uploads[len(uploads)-1].ID, Dir: "next"})
		}

		jsonResponse(w, http.StatusOK, result)
	}
}

// GetUpload returns a single upload by UUID.
//
// @Summary      Get upload by ID
// @Description  Get a single upload by its UUID
// @Tags         Uploads
// @Produce      json
// @Param        id  path  string  true  "Upload UUID"
// @Success      200  {object}  uploadResponse
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id} [get]
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

// QuoteUpload returns an exact cost quote for a file by routing it to antd.
// The caller must send the actual bytes (multipart with a "file" field) — antd
// runs self-encryption + a real quote round-trip with the live network's pricer.
//
// @Summary      Quote upload cost
// @Description  Get an exact cost quote by sending the file bytes. antd runs self-encryption and queries the live network for chunk pricing — no estimation, no scaling. Returns a structured estimated_cost object with cost, chunk_count, gas, and payment_mode.
// @Tags         Uploads
// @Accept       multipart/form-data
// @Produce      json
// @Param        file        formData  file    true   "File to quote"
// @Param        visibility  formData  string  false  "public | private (default private)"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Failure      502  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/quote [post]
func QuoteUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	// antd's quote returns in single-digit seconds once warm, but a cold
	// quote during peer bootstrap can take 2-3 minutes on mainnet.
	client := antd.NewClient(cfg.AntdURL, antd.WithTimeout(300*time.Second))

	return func(w http.ResponseWriter, r *http.Request) {
		// Save the uploaded file to temp, get the exact cost from antd, clean up.
		r.Body = http.MaxBytesReader(w, r.Body, 10<<30)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			jsonError(w, "file too large or invalid form", http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, "file field required", http.StatusBadRequest)
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
		est, err := client.FileCost(r.Context(), tempPath, isPublic)
		if err != nil {
			jsonAntdError(w, "cost estimation failed", err)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"estimated_cost":    est,
			"file_size":         written,
			"original_filename": filepath.Base(header.Filename),
			"visibility":        visibility,
		})
	}
}

// DownloadUpload retrieves a completed upload's data from the Autonomi network.
//
// @Summary      Download completed upload
// @Description  Download a completed upload's file data from the Autonomi network
// @Tags         Uploads
// @Produce      octet-stream
// @Param        id  path  string  true  "Upload UUID"
// @Success      200  {file}    binary
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Failure      502  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id}/download [get]
func DownloadUpload(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	client := antd.NewClient(cfg.AntdURL, antd.WithTimeout(0))

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

		// Download from antd to temp file, then stream to client
		tempDir := worker.TempUploadDir(cfg)
		tempPath := filepath.Join(tempDir, "dl-"+uuid.New().String())
		defer os.Remove(tempPath)

		switch {
		case upload.DataMap.Valid:
			// External signer flow: use local DataMap to download directly
			data, err := client.DataGetPrivate(r.Context(), upload.DataMap.String)
			if err != nil {
				jsonAntdError(w, "download from network failed", err)
				return
			}
			if err := os.WriteFile(tempPath, data, 0600); err != nil {
				jsonError(w, "failed to write download", http.StatusInternalServerError)
				return
			}
		case upload.DatamapAddress.Valid:
			// Legacy flow: download via network address
			if upload.Visibility == "public" {
				if err := client.FileDownloadPublic(r.Context(), upload.DatamapAddress.String, tempPath); err != nil {
					jsonAntdError(w, "download from network failed", err)
					return
				}
			} else {
				data, err := client.DataGetPrivate(r.Context(), upload.DatamapAddress.String)
				if err != nil {
					jsonAntdError(w, "download from network failed", err)
					return
				}
				if err := os.WriteFile(tempPath, data, 0600); err != nil {
					jsonError(w, "failed to write download", http.StatusInternalServerError)
					return
				}
			}
		default:
			jsonError(w, "upload has no data map or network address", http.StatusInternalServerError)
			return
		}

		// Stream the file back
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, upload.OriginalFilename))
		w.Header().Set("Content-Type", upload.ContentType)
		http.ServeFile(w, r, tempPath)
	}
}

// CancelUpload allows a user to cancel their own queued/backoff upload.
//
// @Summary      Cancel queued upload
// @Description  Cancel a queued or backoff upload
// @Tags         Uploads
// @Produce      json
// @Param        id  path  string  true  "Upload UUID"
// @Success      200  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id}/cancel [post]
func CancelUpload(db *sql.DB) http.HandlerFunc {
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

		if err := uploadSvc.Cancel(upload.ID); err != nil {
			jsonError(w, err.Error(), http.StatusConflict)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "upload cancelled"})
	}
}

// RetryUpload resets a failed upload back to queued.
//
// @Summary      Retry failed upload
// @Description  Retry a failed upload by resetting it to queued status
// @Tags         Uploads
// @Produce      json
// @Param        id  path  string  true  "Upload UUID"
// @Success      200  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id}/retry [post]
func RetryUpload(db *sql.DB) http.HandlerFunc {
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

		if err := uploadSvc.Retry(upload.ID); err != nil {
			jsonError(w, err.Error(), http.StatusConflict)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "upload requeued"})
	}
}

// ForceRetryUpload immediately retries an upload in gas backoff.
//
// @Summary      Force retry upload in gas backoff
// @Description  Immediately retry an upload that is in gas backoff state
// @Tags         Uploads
// @Produce      json
// @Param        id  path  string  true  "Upload UUID"
// @Success      200  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id}/force-retry [post]
func ForceRetryUpload(db *sql.DB) http.HandlerFunc {
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

		if err := uploadSvc.ForceRetry(upload.ID); err != nil {
			jsonError(w, err.Error(), http.StatusConflict)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "upload retry forced"})
	}
}

// DeleteUpload permanently removes a user's failed or completed upload record.
//
// @Summary      Delete upload record
// @Description  Permanently delete a failed or completed upload record
// @Tags         Uploads
// @Produce      json
// @Param        id  path  string  true  "Upload UUID"
// @Success      200  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id} [delete]
func DeleteUpload(db *sql.DB) http.HandlerFunc {
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

		if err := uploadSvc.Delete(upload.ID); err != nil {
			jsonError(w, err.Error(), http.StatusConflict)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "upload deleted"})
	}
}

// isAllowedContentType checks the content type against a configurable allowlist.
// Supports wildcard patterns like "image/*". Returns true if no allowlist is configured.
func isAllowedContentType(ct string, settingsSvc *services.SettingsService) bool {
	allowlist := "image/*,application/pdf,text/*,application/json,application/zip,application/gzip,application/x-tar,video/*,audio/*,application/octet-stream"
	if v, err := settingsSvc.Get("allowed_upload_content_types"); err == nil && v != "" {
		allowlist = v
	}

	ct = strings.ToLower(strings.TrimSpace(ct))
	// Strip parameters (e.g., "text/plain; charset=utf-8" → "text/plain")
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}

	for _, pattern := range strings.Split(allowlist, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == ct {
			return true
		}
		// Wildcard: "image/*" matches "image/png"
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(ct, prefix) {
				return true
			}
		}
	}
	return false
}
