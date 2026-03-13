package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/maidsafe/indelible/internal/services"
)

func parseSince(r *http.Request) time.Time {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 7 // default 7 days
	}
	return time.Now().AddDate(0, 0, -days)
}

// AdminUploadAnalytics returns upload statistics.
func AdminUploadAnalytics(db *sql.DB) http.HandlerFunc {
	analyticsSvc := services.NewAnalyticsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		since := parseSince(r)
		stats, err := analyticsSvc.UploadAnalytics(since)
		if err != nil {
			jsonError(w, "failed to get upload analytics", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, stats)
	}
}

// AdminTokenAnalytics returns token usage statistics.
func AdminTokenAnalytics(db *sql.DB) http.HandlerFunc {
	analyticsSvc := services.NewAnalyticsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		since := parseSince(r)
		stats, err := analyticsSvc.TokenAnalytics(since)
		if err != nil {
			jsonError(w, "failed to get token analytics", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, stats)
	}
}

// AdminCostAnalytics returns cost analytics by token and department.
func AdminCostAnalytics(db *sql.DB) http.HandlerFunc {
	analyticsSvc := services.NewAnalyticsService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		since := parseSince(r)
		stats, err := analyticsSvc.CostAnalytics(since)
		if err != nil {
			jsonError(w, "failed to get cost analytics", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, stats)
	}
}
