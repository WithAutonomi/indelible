package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

func parseSince(r *http.Request) time.Time {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 7 // default 7 days
	}
	return time.Now().AddDate(0, 0, -days)
}

// @Summary      Get upload analytics
// @Description  Return upload statistics for the specified time period
// @Tags         Admin: Analytics
// @Produce      json
// @Param        days query int false "Number of days to look back (default 7)"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/analytics/uploads [get]
// @Security     BearerAuth
// AdminUploadAnalytics returns upload statistics.
func AdminUploadAnalytics(db *database.DB) http.HandlerFunc {
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

// @Summary      Get token analytics
// @Description  Return API token usage statistics for the specified time period
// @Tags         Admin: Analytics
// @Produce      json
// @Param        days query int false "Number of days to look back (default 7)"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/analytics/tokens [get]
// @Security     BearerAuth
// AdminTokenAnalytics returns token usage statistics.
func AdminTokenAnalytics(db *database.DB) http.HandlerFunc {
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

// @Summary      Get cost analytics
// @Description  Return cost analytics broken down by token and department
// @Tags         Admin: Analytics
// @Produce      json
// @Param        days query int false "Number of days to look back (default 7)"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} map[string]string
// @Router       /admin/analytics/costs [get]
// @Security     BearerAuth
// AdminCostAnalytics returns cost analytics by token and department.
func AdminCostAnalytics(db *database.DB) http.HandlerFunc {
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
