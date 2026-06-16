package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/WithAutonomi/indelible/internal/database"
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
func CreateUpload(db *database.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	tagSvc := services.NewTagService(db)
	tagRuleSvc := services.NewTagRuleService(db)
	quotaSvc := services.NewQuotaService(db)
	webhookSvc := services.NewWebhookDeliveryService(db)
	settingsSvc := services.NewSettingsService(db)
	userSvc := services.NewUserService(db)
	tokenSvc := services.NewTokenService(db)
	logSvc := services.NewLogService(db)

	walletSvc := services.NewWalletService(db, cfg.WalletKeyring())

	return func(w http.ResponseWriter, r *http.Request) {
		// Pre-flight: reject early if no wallet is configured
		wallet, err := walletSvc.GetDefault()
		if err != nil || wallet == nil {
			jsonErrorWithCode(w, "No wallet configured", "wallet_not_configured", http.StatusServiceUnavailable)
			return
		}

		// Disk back-pressure: the disk-alert worker sets "uploads_paused" when the
		// data dir crosses the critical threshold. Shed load here before buffering
		// a temp file we'd only fail to write — mirrors the queue_full path below.
		if paused, err := settingsSvc.Get("uploads_paused"); err == nil && paused == "true" {
			w.Header().Set("Retry-After", "300")
			jsonErrorWithCode(w, "Uploads are temporarily paused due to low disk space", "uploads_paused", http.StatusServiceUnavailable)
			return
		}

		// Network back-pressure: the system monitor sets "antd_unavailable" when
		// antd is persistently unreachable (debounced). Fast-fail here rather than
		// buffering a temp file and queueing an upload that can't be stored —
		// same shed-load shape as the disk-pressure check above (V2-486).
		if down, err := settingsSvc.Get("antd_unavailable"); err == nil && down == "true" {
			w.Header().Set("Retry-After", "30")
			jsonErrorWithCode(w, "The Autonomi network is currently unreachable, please try again shortly", "network_unavailable", http.StatusServiceUnavailable)
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

		// S14: Read max_upload_size_bytes from settings (the system-level ceiling)
		systemMaxSize := int64(10 << 30) // 10 GB fallback
		if maxStr, err := settingsSvc.Get("max_upload_size_bytes"); err == nil {
			if n, err := strconv.ParseInt(maxStr, 10, 64); err == nil && n > 0 {
				systemMaxSize = n
			}
		}

		// V2-327: resolve effective upload restrictions — token override > user
		// override > system default. For Bearer JWT (web UI) requests tokenID is 0.
		var tokenRec *services.Token
		if tokenID != 0 {
			if t, err := tokenSvc.GetByID(tokenID); err == nil {
				tokenRec = t
			}
		}
		userRec, err := userSvc.GetByID(userID)
		if err != nil {
			jsonError(w, "failed to load user", http.StatusInternalServerError)
			return
		}

		maxUploadSize := systemMaxSize
		switch {
		case tokenRec != nil && tokenRec.MaxFileSizeBytes.Valid && tokenRec.MaxFileSizeBytes.Int64 > 0:
			if tokenRec.MaxFileSizeBytes.Int64 < maxUploadSize {
				maxUploadSize = tokenRec.MaxFileSizeBytes.Int64
			}
		case userRec.MaxFileSizeBytes.Valid && userRec.MaxFileSizeBytes.Int64 > 0:
			if userRec.MaxFileSizeBytes.Int64 < maxUploadSize {
				maxUploadSize = userRec.MaxFileSizeBytes.Int64
			}
		}

		// Limit request body size
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

		// Parse multipart form. Differentiate "body exceeds limit" (413) from
		// other parse errors (400 — e.g. missing/invalid form).
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB memory buffer
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				jsonErrorWithCode(w, "file exceeds maximum upload size", "file_too_large", http.StatusRequestEntityTooLarge)
				return
			}
			jsonErrorWithCode(w, "invalid multipart form", "invalid_request", http.StatusBadRequest)
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

		// Detect and validate content type. Token override > user override > system list.
		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		allowlist := effectiveAllowlist(tokenRec, userRec, settingsSvc)
		if !matchContentType(contentType, allowlist) {
			jsonError(w, "file type not allowed: "+contentType, http.StatusUnsupportedMediaType)
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

		// Belt-and-braces — MaxBytesReader covers the request body but the
		// effective per-token/per-user cap can still be smaller than the
		// system limit and worth surfacing as 413 with a clear code.
		if written > maxUploadSize {
			os.Remove(tempPath)
			jsonErrorWithCode(w, "file exceeds maximum upload size", "file_too_large", http.StatusRequestEntityTooLarge)
			return
		}

		// Create upload record
		var tID *int64
		if tokenID != 0 {
			tID = &tokenID
		}

		// Check quota before accepting upload. Pass tokenID so the department
		// tier can resolve the token's department; nil for web-UI uploads.
		if err := quotaSvc.CheckUserQuota(userID, tID, written); err != nil {
			os.Remove(tempPath)
			auditEvent(r, logSvc, "file_upload_denied", "warn", &userID,
				fmt.Sprintf("reason=quota_exceeded filename=%s size=%d", originalFilename, written))
			jsonErrorWithCode(w, "quota exceeded: "+err.Error(), "quota_exceeded", http.StatusForbidden)
			return
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

		auditEvent(r, logSvc, "file_uploaded", "info", &userID,
			fmt.Sprintf("uuid=%s filename=%s size=%d visibility=%s", upload.UUID, originalFilename, written, visibility))

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
func ListUploads(db *database.DB) http.HandlerFunc {
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
func GetUpload(db *database.DB) http.HandlerFunc {
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
func QuoteUpload(db *database.DB, cfg *config.Config) http.HandlerFunc {
	// antd's quote returns in single-digit seconds once warm, but a cold
	// quote during peer bootstrap can take 2-3 minutes on mainnet. The
	// effective ceiling is read per-request from the antd_quote_timeout_secs
	// setting (default 300, bounds 1-3600) so operators can tune without a
	// rebuild — the cached settings service absorbs the DB hit.
	settingsSvc := services.NewCachedSettingsService(services.NewSettingsService(db))

	return func(w http.ResponseWriter, r *http.Request) {
		timeout := time.Duration(settingsSvc.GetIntWithBounds(
			"antd_quote_timeout_secs", 300, 1, 3600,
		)) * time.Second
		client := antd.NewClient(cfg.AntdURL, antd.WithTimeout(timeout))

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
		est, err := client.FileCost(r.Context(), tempPath, isPublic, antd.PaymentModeAuto)
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

// downloadCacheControl is the Cache-Control for a successful download (V2-516).
// Autonomi content is immutable and content-addressed, so the bytes at a given
// upload URL never change → cache effectively forever. `private` because the
// route is token-gated (no anonymous access): a shared cache must not reuse a
// response across identities, but a trusted-boundary proxy or the client may.
const downloadCacheControl = "private, max-age=31536000, immutable"

// downloadETag returns a strong, content-derived ETag for an upload's bytes, or
// "" when no content identifier is available. The identifier (local DataMap or
// network address) is content-addressed, so a hash of it is a stable validator.
// It is hashed — not emitted raw — so the ETag never leaks the DataMap, which
// is itself the capability needed to retrieve the file.
func downloadETag(u *services.Upload) string {
	var id string
	switch {
	case u.DataMap.Valid && u.DataMap.String != "":
		id = u.DataMap.String
	case u.DatamapAddress.Valid && u.DatamapAddress.String != "":
		id = u.DatamapAddress.String
	default:
		return ""
	}
	sum := sha256.Sum256([]byte("indelible-download-v1:" + id))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// etagMatches reports whether an If-None-Match header matches the given strong
// ETag. Handles "*", comma-separated lists, and the W/ weak-validator prefix
// (compared as weak — safe here since the content is immutable).
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	for _, c := range strings.Split(ifNoneMatch, ",") {
		c = strings.TrimSpace(c)
		c = strings.TrimPrefix(c, "W/")
		if c == etag {
			return true
		}
	}
	return false
}

// DownloadUpload retrieves a completed upload's data from the Autonomi network.
//
// @Summary      Download completed upload
// @Description  Download a completed upload's file data from the Autonomi network
// @Tags         Uploads
// @Produce      octet-stream
// @Param        id              path    string  true   "Upload UUID"
// @Param        If-None-Match   header  string  false  "ETag for conditional GET; returns 304 if unchanged"
// @Success      200  {file}    binary
// @Success      304  {string}  string  "Not Modified — cached copy is current"
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Failure      502  {object}  map[string]string
// @Security     BearerAuth
// @Router       /uploads/{id}/download [get]
func DownloadUpload(db *database.DB, cfg *config.Config) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	logSvc := services.NewLogService(db)
	settingsSvc := services.NewCachedSettingsService(services.NewSettingsService(db))

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
			// Log the denied cross-user access attempt (returns 404 to the client
			// to avoid leaking existence, but the attempt is security-relevant).
			// Download-route events go to the plain file_access_log, not the audit
			// hash-chain (V2-514), so reader replicas stay chain-free.
			fileAccessEvent(r, logSvc, "file_download_denied", "warn", &userID,
				fmt.Sprintf("uuid=%s reason=not_owner", uploadUUID))
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		// "already_stored" (content-addressed dedup) is just as retrievable as
		// "completed" — both have a usable DataMap/address on the network.
		if upload.Status != "completed" && upload.Status != "already_stored" {
			jsonError(w, fmt.Sprintf("upload is %s, not ready for download", upload.Status), http.StatusConflict)
			return
		}

		// Conditional request (V2-516): the bytes at this URL are immutable —
		// Autonomi content is content-addressed and an upload's data never changes
		// — so if the client already holds the current ETag we can answer 304 and
		// skip the antd fetch entirely. Computed only now, after the owner + status
		// checks, so a 304 is never returned to an unauthorized caller. The success
		// path sets the same ETag/Cache-Control headers just before streaming (we
		// avoid setting them earlier so a later antd error isn't sent cacheable).
		etag := downloadETag(upload)
		if etag != "" && etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", downloadCacheControl)
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Download from antd to temp file, then stream to client. All branches
		// use the streaming FileGet*/ primitives so the daemon writes straight to
		// tempPath — the file bytes never enter indelible's heap, so peak RAM is
		// bounded independent of file size (private downloads previously buffered
		// the whole file via DataGet → []byte, an OOM/DoS vector on large files).
		tempDir := worker.TempUploadDir(cfg)
		tempPath := filepath.Join(tempDir, "dl-"+uuid.New().String())
		defer os.Remove(tempPath)

		// Bound the download so a slow or oversized transfer can't hold the
		// handler (and its temp file) open indefinitely. Operator-tunable like
		// the quote timeout; default 30m is generous for large files on mainnet.
		timeout := time.Duration(settingsSvc.GetIntWithBounds(
			"antd_download_timeout_secs", 1800, 1, 86400,
		)) * time.Second
		client := antd.NewClient(cfg.AntdURL, antd.WithTimeout(timeout))

		switch {
		case upload.DataMap.Valid:
			// External signer flow: use local DataMap to download directly
			if err := client.FileGet(r.Context(), upload.DataMap.String, tempPath); err != nil {
				jsonAntdError(w, "download from network failed", err)
				return
			}
		case upload.DatamapAddress.Valid:
			// Legacy flow: download via network address
			if upload.Visibility == "public" {
				if err := client.FileGetPublic(r.Context(), upload.DatamapAddress.String, tempPath); err != nil {
					jsonAntdError(w, "download from network failed", err)
					return
				}
			} else {
				if err := client.FileGet(r.Context(), upload.DatamapAddress.String, tempPath); err != nil {
					jsonAntdError(w, "download from network failed", err)
					return
				}
			}
		default:
			jsonError(w, "upload has no data map or network address", http.StatusInternalServerError)
			return
		}

		// Record the successful access decision before streaming the bytes. This
		// high-volume read telemetry goes to the plain file_access_log rather than
		// the audit hash-chain (V2-514) so concurrent downloads across a reader
		// fleet neither fork the chain nor serialize on its mutex.
		fileAccessEvent(r, logSvc, "file_downloaded", "info", &userID,
			fmt.Sprintf("uuid=%s filename=%s visibility=%s", upload.UUID, upload.OriginalFilename, upload.Visibility))

		// Stream the file back. Content-Disposition: attachment already forces a
		// download (no inline render); X-Content-Type-Options: nosniff stops the
		// browser from MIME-sniffing the bytes into an executable type, closing
		// the stored-XSS-on-open vector if a stored Content-Type is risky (V2-433 A3.6).
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, upload.OriginalFilename))
		w.Header().Set("Content-Type", upload.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Cache validators (V2-516) on the success path only — immutable,
		// content-addressed bytes, so a long-lived strong ETag is safe and lets a
		// trusted-boundary proxy or the client serve repeats without re-fetching
		// from antd. `private`: downloads are token-gated (no anonymous route), so
		// a shared cache must not reuse a response across identities.
		if etag != "" {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", downloadCacheControl)
		}
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
func CancelUpload(db *database.DB) http.HandlerFunc {
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
func RetryUpload(db *database.DB) http.HandlerFunc {
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
func ForceRetryUpload(db *database.DB) http.HandlerFunc {
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
func DeleteUpload(db *database.DB) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	logSvc := services.NewLogService(db)

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
			auditEvent(r, logSvc, "file_delete_denied", "warn", &userID,
				fmt.Sprintf("uuid=%s reason=not_owner", uploadUUID))
			jsonError(w, "upload not found", http.StatusNotFound)
			return
		}

		if err := uploadSvc.Delete(upload.ID); err != nil {
			jsonError(w, err.Error(), http.StatusConflict)
			return
		}

		auditEvent(r, logSvc, "file_deleted", "info", &userID,
			fmt.Sprintf("uuid=%s filename=%s", upload.UUID, upload.OriginalFilename))

		jsonResponse(w, http.StatusOK, map[string]string{"message": "upload deleted"})
	}
}

// effectiveAllowlist resolves the content-type allowlist for an upload using
// the override chain: token > user > system setting > built-in default.
// Returns a comma-separated string of patterns (same shape as the setting).
func effectiveAllowlist(tok *services.Token, user *services.User, settingsSvc *services.SettingsService) string {
	if tok != nil && tok.AllowedFileTypes.Valid && tok.AllowedFileTypes.String != "" {
		if csv := jsonArrayToCSV(tok.AllowedFileTypes.String); csv != "" {
			return csv
		}
	}
	if user != nil && user.AllowedFileTypes.Valid && user.AllowedFileTypes.String != "" {
		if csv := jsonArrayToCSV(user.AllowedFileTypes.String); csv != "" {
			return csv
		}
	}
	if v, err := settingsSvc.Get("allowed_upload_content_types"); err == nil && v != "" {
		return v
	}
	return "image/*,application/pdf,text/*,application/json,application/zip,application/gzip,application/x-tar,video/*,audio/*,application/octet-stream"
}

// jsonArrayToCSV turns a stored JSON string array like `["image/*","application/pdf"]`
// into the comma-separated form matchContentType expects. Returns "" on parse error
// or when the array is empty — callers should treat that as "no restriction at this tier".
func jsonArrayToCSV(s string) string {
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return ""
	}
	return strings.Join(arr, ",")
}

// matchContentType checks ct against a comma-separated list of patterns.
// Supports wildcard patterns like "image/*". An empty allowlist matches nothing.
func matchContentType(ct, allowlist string) bool {
	if strings.TrimSpace(allowlist) == "" {
		return false
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}

	for _, pattern := range strings.Split(allowlist, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if pattern == ct {
			return true
		}
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(ct, prefix) {
				return true
			}
		}
	}
	return false
}
