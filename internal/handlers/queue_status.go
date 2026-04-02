package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/WithAutonomi/indelible/internal/services"
)

// QueueStatus returns the current upload queue state for backpressure signaling.
func QueueStatus(db *sql.DB) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	settingsSvc := services.NewSettingsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		counts, err := uploadSvc.CountByStatus()
		if err != nil {
			jsonError(w, "failed to query queue status", http.StatusInternalServerError)
			return
		}

		maxQueued := int64(500)
		if v, err := settingsSvc.Get("max_queued_uploads"); err == nil {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				maxQueued = n
			}
		}

		maxConcurrent := int64(4)
		if v, err := settingsSvc.Get("max_concurrent_uploads"); err == nil {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				maxConcurrent = n
			}
		}

		queued := counts["queued"]
		processing := counts["processing"]
		completed := counts["completed"]
		failed := counts["failed"]

		// Rough estimate: ~10s per upload at current concurrency
		var estimatedWaitMin float64
		if maxConcurrent > 0 && queued > 0 {
			estimatedWaitMin = float64(queued) * 10.0 / float64(maxConcurrent) / 60.0
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"queued":               queued,
			"processing":           processing,
			"completed":            completed,
			"failed":               failed,
			"max_queued":           maxQueued,
			"max_concurrent":       maxConcurrent,
			"queue_available":      maxQueued - queued - processing,
			"estimated_wait_minutes": estimatedWaitMin,
		})
	}
}
