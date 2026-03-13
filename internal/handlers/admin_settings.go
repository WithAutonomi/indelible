package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/maidsafe/indelible/internal/middleware"
	"github.com/maidsafe/indelible/internal/services"
)

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

		ipAddress := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ipAddress = fwd
		}
		userAgent := r.Header.Get("User-Agent")

		if err := settingsSvc.Update(changes, userID, ipAddress, userAgent); err != nil {
			jsonError(w, "failed to update settings", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "settings updated"})
	}
}

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

// AdminImportSettings restores settings from a JSON export.
func AdminImportSettings(db *sql.DB) http.HandlerFunc {
	settingsSvc := services.NewSettingsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var data map[string]string
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		ipAddress := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ipAddress = fwd
		}
		userAgent := r.Header.Get("User-Agent")

		if err := settingsSvc.Import(data, userID, ipAddress, userAgent); err != nil {
			jsonError(w, "failed to import settings", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"message":  "settings imported",
			"imported": len(data),
		})
	}
}
