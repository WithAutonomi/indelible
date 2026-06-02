package middleware

import (
	"net/http"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

// MaintenanceMode returns 503 Service Unavailable when maintenance mode is
// enabled — except for admins, who pass through so they can turn it back off.
//
// It MUST be mounted after Authenticate so the caller's identity is on the
// request context; without that an admin would be 503'd alongside everyone
// else and the only recovery from maintenance mode would be editing the
// settings table directly in the DB. Public auth routes (login etc.) are
// deliberately left unwrapped by this middleware so an admin can always sign
// in to disable maintenance mode.
func MaintenanceMode(db *database.DB) func(http.Handler) http.Handler {
	settingsSvc := services.NewSettingsService(db)
	permSvc := services.NewPermissionService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mode, _ := settingsSvc.Get("maintenance_mode")
			if mode == "true" {
				// Admins are exempt — they need the off-switch.
				if userID := GetUserID(r.Context()); userID != 0 {
					if isAdmin, err := permSvc.IsAdmin(userID); err == nil && isAdmin {
						next.ServeHTTP(w, r)
						return
					}
				}
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
