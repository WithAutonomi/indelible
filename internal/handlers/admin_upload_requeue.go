package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
	"github.com/WithAutonomi/indelible/internal/worker"
)

// UploadRequeueWindow is the stuck-upload requeue window in force on this
// instance (V2-1773). An upload with no progress for this long is put back in
// the queue; uploads this instance is still processing are never requeued.
type UploadRequeueWindow struct {
	Minutes        int  `json:"minutes"`         // window in force
	DefaultMinutes int  `json:"default_minutes"` // built-in default, restored on restart
	MinMinutes     int  `json:"min_minutes"`     // smallest accepted override
	MaxMinutes     int  `json:"max_minutes"`     // largest accepted override
	Overridden     bool `json:"overridden"`      // an operator override is in force (memory only)
	WorkersEnabled bool `json:"workers_enabled"` // false on a reader instance: no upload worker to tune
}

// UploadRequeueWindowRequest sets the window, in minutes.
type UploadRequeueWindowRequest struct {
	Minutes int `json:"minutes"`
}

func currentUploadRequeueWindow(cfg *config.Config) UploadRequeueWindow {
	window, overridden := worker.StuckRequeueAfter()
	return UploadRequeueWindow{
		Minutes:        int(window / time.Minute),
		DefaultMinutes: int(worker.DefaultStuckRequeueAfter / time.Minute),
		MinMinutes:     int(worker.MinStuckRequeueAfter / time.Minute),
		MaxMinutes:     int(worker.MaxStuckRequeueAfter / time.Minute),
		Overridden:     overridden,
		WorkersEnabled: cfg.WorkersEnabled,
	}
}

// @Summary      Get the stuck-upload requeue window
// @Description  How long an upload may make no progress before the upload worker puts it back in the queue. Uploads this instance is still processing are never requeued. An override set here lives in memory only and reverts to the default on restart.
// @Tags         Admin: System
// @Produce      json
// @Success      200 {object} handlers.UploadRequeueWindow
// @Router       /admin/upload-requeue-window [get]
// @Security     BearerAuth
func AdminGetUploadRequeueWindow(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, currentUploadRequeueWindow(cfg))
	}
}

// @Summary      Set the stuck-upload requeue window
// @Description  Override the requeue window on this instance until it restarts, e.g. to let a long upload through. Audited. Refused on an instance that runs no upload workers.
// @Tags         Admin: System
// @Accept       json
// @Produce      json
// @Param        body body handlers.UploadRequeueWindowRequest true "Window in minutes"
// @Success      200 {object} handlers.UploadRequeueWindow
// @Failure      400 {object} map[string]string
// @Failure      409 {object} map[string]string
// @Router       /admin/upload-requeue-window [put]
// @Security     BearerAuth
func AdminSetUploadRequeueWindow(cfg *config.Config, db *database.DB) http.HandlerFunc {
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.WorkersEnabled {
			jsonError(w, "this instance runs no upload workers (reader role); set the window on the writer", http.StatusConflict)
			return
		}
		var req UploadRequeueWindowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		before := currentUploadRequeueWindow(cfg)
		if err := worker.SetStuckRequeueAfter(time.Duration(req.Minutes) * time.Minute); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		after := currentUploadRequeueWindow(cfg)
		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "upload_requeue_window_changed", "warn", &callerID,
			fmt.Sprintf("minutes=%d (was %d); in memory until restart", after.Minutes, before.Minutes))
		jsonResponse(w, http.StatusOK, after)
	}
}

// @Summary      Reset the stuck-upload requeue window
// @Description  Drop the operator override on this instance, restoring the default window. Audited. Refused on an instance that runs no upload workers.
// @Tags         Admin: System
// @Produce      json
// @Success      200 {object} handlers.UploadRequeueWindow
// @Failure      409 {object} map[string]string
// @Router       /admin/upload-requeue-window [delete]
// @Security     BearerAuth
func AdminResetUploadRequeueWindow(cfg *config.Config, db *database.DB) http.HandlerFunc {
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.WorkersEnabled {
			jsonError(w, "this instance runs no upload workers (reader role); set the window on the writer", http.StatusConflict)
			return
		}
		before := currentUploadRequeueWindow(cfg)
		worker.ResetStuckRequeueAfter()
		after := currentUploadRequeueWindow(cfg)
		callerID := middleware.GetUserID(r.Context())
		auditEvent(r, logSvc, "upload_requeue_window_changed", "warn", &callerID,
			fmt.Sprintf("reset to default minutes=%d (was %d)", after.Minutes, before.Minutes))
		jsonResponse(w, http.StatusOK, after)
	}
}
