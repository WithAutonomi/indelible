package middleware

import (
	"database/sql"
	"net/http"

	"github.com/WithAutonomi/indelible/internal/services"
)

// MaintenanceMode checks the system settings for maintenance mode and returns
// 503 Service Unavailable if enabled. Health and admin routes are exempt.
func MaintenanceMode(db *sql.DB) func(http.Handler) http.Handler {
	settingsSvc := services.NewSettingsService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mode, _ := settingsSvc.Get("maintenance_mode")
			if mode == "true" {
				msg, _ := settingsSvc.Get("maintenance_message")
				if msg == "" {
					msg = "System is under maintenance. Please try again later."
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error":"` + msg + `","code":"maintenance_mode"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
