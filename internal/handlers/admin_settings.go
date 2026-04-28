package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// @Summary      Get all settings
// @Description  Return all system configuration settings
// @Tags         Admin: Settings
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/settings [get]
// @Security     BearerAuth
// AdminGetSettings returns all system settings.
func AdminGetSettings(db *sql.DB) http.HandlerFunc {
	settingsSvc := services.NewSettingsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := settingsSvc.GetAll()
		if err != nil {
			jsonError(w, "failed to get settings", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"settings": settings})
	}
}

// @Summary      Update settings
// @Description  Apply partial updates to system settings with audit logging
// @Tags         Admin: Settings
// @Accept       json
// @Produce      json
// @Param        body body map[string]string true "Key-value pairs to update"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/settings [patch]
// @Security     BearerAuth
// AdminUpdateSettings applies partial updates to settings with audit logging.
func AdminUpdateSettings(db *sql.DB) http.HandlerFunc {
	settingsSvc := services.NewSettingsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var changes map[string]string
		if err := json.NewDecoder(r.Body).Decode(&changes); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if len(changes) == 0 {
			jsonError(w, "no settings to update", http.StatusBadRequest)
			return
		}

		ipAddress := middleware.ClientIP(r, nil)
		userAgent := r.Header.Get("User-Agent")

		if err := settingsSvc.Update(changes, userID, ipAddress, userAgent); err != nil {
			var verr *services.ValidationError
			if errors.As(err, &verr) {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}
			jsonError(w, "failed to update settings", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "settings updated"})
	}
}

// @Summary      Export settings
// @Description  Download all settings as a JSON file for backup or migration
// @Tags         Admin: Settings
// @Produce      application/json
// @Success      200 {file} file "JSON settings export"
// @Failure      500 {object} map[string]string
// @Router       /admin/settings/export [get]
// @Security     BearerAuth
// AdminExportSettings returns all settings as a downloadable JSON file.
func AdminExportSettings(db *sql.DB) http.HandlerFunc {
	settingsSvc := services.NewSettingsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		data, err := settingsSvc.Export()
		if err != nil {
			jsonError(w, "failed to export settings", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="indelible-settings.json"`)
		w.Write(data)
	}
}

// @Summary      Import settings
// @Description  Restore settings from a JSON export (supports structured and legacy flat formats)
// @Tags         Admin: Settings
// @Accept       json
// @Produce      json
// @Param        body body object true "Settings export JSON"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/settings/import [post]
// @Security     BearerAuth
// AdminImportSettings restores settings from a JSON export.
func AdminImportSettings(db *sql.DB) http.HandlerFunc {
	settingsSvc := services.NewSettingsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		ipAddress := middleware.ClientIP(r, nil)
		userAgent := r.Header.Get("User-Agent")

		// Try structured format first (has "settings" key)
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Detect format: structured has a "settings" object, legacy is flat key-value
		var structured services.ExportData
		if err := json.Unmarshal(raw, &structured); err == nil && structured.Settings != nil {
			if err := settingsSvc.ImportStructured(&structured, userID, ipAddress, userAgent); err != nil {
				jsonError(w, "failed to import settings", http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{
				"message":  "settings imported",
				"settings": len(structured.Settings),
				"webhooks": len(structured.Webhooks),
				"groups":   len(structured.Groups),
			})
			return
		}

		// Legacy flat format
		var flat map[string]string
		if err := json.Unmarshal(raw, &flat); err != nil {
			jsonError(w, "invalid JSON format", http.StatusBadRequest)
			return
		}

		if err := settingsSvc.Import(flat, userID, ipAddress, userAgent); err != nil {
			jsonError(w, "failed to import settings", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"message":  "settings imported",
			"settings": len(flat),
		})
	}
}
