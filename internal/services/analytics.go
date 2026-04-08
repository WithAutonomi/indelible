package services

import (
	"database/sql"
	"fmt"
	"time"
)

// UploadStats holds upload analytics.
type UploadStats struct {
	TotalUploads    int64            `json:"total_uploads"`
	StatusCounts    map[string]int64 `json:"status_counts"`
	TotalBytes      int64            `json:"total_bytes"`
	AvgFileSize     int64            `json:"avg_file_size"`
	AvgProcessingMs int64            `json:"avg_processing_ms"`
	TopUploaders    []UploaderStat   `json:"top_uploaders"`
	RecentFailures  []FailureStat    `json:"recent_failures"`
}

// UploaderStat is a per-user upload count.
type UploaderStat struct {
	UserID     int64  `json:"user_id"`
	Email      string `json:"email"`
	Count      int64  `json:"count"`
	TotalBytes int64  `json:"total_bytes"`
}

// FailureStat is a recent failed upload.
type FailureStat struct {
	UUID         string `json:"uuid"`
	Filename     string `json:"original_filename"`
	ErrorMessage string `json:"error_message"`
	FailedAt     string `json:"failed_at"`
}

// TokenStats holds token usage analytics.
type TokenStats struct {
	TotalRequests int64             `json:"total_requests"`
	ActiveTokens  int64             `json:"active_tokens"`
	TopTokens     []TokenUsageStat  `json:"top_tokens"`
}

// TokenUsageStat is per-token usage.
type TokenUsageStat struct {
	TokenUUID  string `json:"token_uuid"`
	TokenName  string `json:"token_name"`
	Requests   int64  `json:"requests"`
	LastUsedAt string `json:"last_used_at"`
}

// CostStats holds cost analytics.
type CostStats struct {
	TotalCost       string            `json:"total_cost"`
	TotalUploads    int64             `json:"total_uploads"`
	TotalBytes      int64             `json:"total_bytes"`
	AvgCostPerUpload string           `json:"avg_cost_per_upload"`
	ByDepartment    []DepartmentCost  `json:"by_department"`
	ByToken         []TokenCost       `json:"by_token"`
}

// DepartmentCost is per-department cost aggregation.
type DepartmentCost struct {
	Department  string `json:"department"`
	TotalCost   string `json:"total_cost"`
	UploadCount int64  `json:"upload_count"`
	TotalBytes  int64  `json:"total_bytes"`
}

// TokenCost is per-token cost aggregation.
type TokenCost struct {
	TokenUUID   string `json:"token_uuid"`
	TokenName   string `json:"token_name"`
	TotalCost   string `json:"total_cost"`
	UploadCount int64  `json:"upload_count"`
	TotalBytes  int64  `json:"total_bytes"`
}

// AnalyticsService provides analytics queries.
type AnalyticsService struct {
	db *sql.DB
}

// NewAnalyticsService creates a new AnalyticsService.
func NewAnalyticsService(db *sql.DB) *AnalyticsService {
	return &AnalyticsService{db: db}
}

// UploadAnalytics returns upload statistics since the given time.
func (s *AnalyticsService) UploadAnalytics(since time.Time) (*UploadStats, error) {
	sinceStr := since.Format("2006-01-02T15:04:05")
	stats := &UploadStats{StatusCounts: make(map[string]int64)}

	// Total and status counts
	rows, err := s.db.Query(
		`SELECT status, COUNT(*), COALESCE(SUM(file_size), 0) FROM uploads WHERE created_at >= ? GROUP BY status`, sinceStr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count, bytes int64
		if err := rows.Scan(&status, &count, &bytes); err != nil {
			return nil, err
		}
		stats.StatusCounts[status] = count
		stats.TotalUploads += count
		stats.TotalBytes += bytes
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if stats.TotalUploads > 0 {
		stats.AvgFileSize = stats.TotalBytes / stats.TotalUploads
	}

	// Average processing time (completed uploads only)
	s.db.QueryRow(
		`SELECT COALESCE(AVG(
			(julianday(completed_at) - julianday(processing_at)) * 86400000
		), 0) FROM uploads WHERE status = 'completed' AND completed_at IS NOT NULL AND processing_at IS NOT NULL AND created_at >= ?`,
		sinceStr,
	).Scan(&stats.AvgProcessingMs)

	// Top uploaders
	uploaderRows, err := s.db.Query(
		`SELECT u.user_id, COALESCE(usr.email, ''), COUNT(*), COALESCE(SUM(u.file_size), 0)
		 FROM uploads u LEFT JOIN users usr ON u.user_id = usr.id AND usr.deleted_at IS NULL
		 WHERE u.created_at >= ?
		 GROUP BY u.user_id ORDER BY COUNT(*) DESC LIMIT 10`, sinceStr,
	)
	if err != nil {
		return nil, err
	}
	defer uploaderRows.Close()

	for uploaderRows.Next() {
		var us UploaderStat
		if err := uploaderRows.Scan(&us.UserID, &us.Email, &us.Count, &us.TotalBytes); err != nil {
			return nil, err
		}
		stats.TopUploaders = append(stats.TopUploaders, us)
	}

	// Recent failures
	failRows, err := s.db.Query(
		`SELECT uuid, original_filename, COALESCE(error_message, ''), COALESCE(failed_at, created_at)
		 FROM uploads WHERE status = 'failed' AND created_at >= ?
		 ORDER BY failed_at DESC LIMIT 10`, sinceStr,
	)
	if err != nil {
		return nil, err
	}
	defer failRows.Close()

	for failRows.Next() {
		var f FailureStat
		var failedAt string
		if err := failRows.Scan(&f.UUID, &f.Filename, &f.ErrorMessage, &failedAt); err != nil {
			return nil, err
		}
		// Parse the datetime string from SQLite/Postgres; normalize to RFC3339-like output
		if t, err := time.Parse("2006-01-02 15:04:05", failedAt); err == nil {
			f.FailedAt = t.Format("2006-01-02T15:04:05Z")
		} else if t, err := time.Parse("2006-01-02T15:04:05Z", failedAt); err == nil {
			f.FailedAt = t.Format("2006-01-02T15:04:05Z")
		} else {
			f.FailedAt = failedAt
		}
		stats.RecentFailures = append(stats.RecentFailures, f)
	}

	return stats, nil
}

// TokenAnalytics returns token usage statistics since the given time.
func (s *AnalyticsService) TokenAnalytics(since time.Time) (*TokenStats, error) {
	sinceStr := since.Format("2006-01-02T15:04:05")
	stats := &TokenStats{}

	// Total requests
	s.db.QueryRow(
		`SELECT COUNT(*) FROM token_usage_log WHERE created_at >= ?`, sinceStr,
	).Scan(&stats.TotalRequests)

	// Active tokens (tokens with at least one request in the period)
	s.db.QueryRow(
		`SELECT COUNT(DISTINCT token_id) FROM token_usage_log WHERE created_at >= ?`, sinceStr,
	).Scan(&stats.ActiveTokens)

	// Top tokens by request count
	rows, err := s.db.Query(
		`SELECT t.uuid, t.name, COUNT(l.id) as reqs, MAX(l.created_at) as last_used
		 FROM token_usage_log l
		 JOIN api_tokens t ON l.token_id = t.id
		 WHERE l.created_at >= ?
		 GROUP BY t.id ORDER BY reqs DESC LIMIT 10`, sinceStr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ts TokenUsageStat
		var lastUsed time.Time
		if err := rows.Scan(&ts.TokenUUID, &ts.TokenName, &ts.Requests, &lastUsed); err != nil {
			return nil, err
		}
		ts.LastUsedAt = lastUsed.Format("2006-01-02T15:04:05Z")
		stats.TopTokens = append(stats.TopTokens, ts)
	}

	return stats, rows.Err()
}

// CostAnalytics returns cost statistics since the given time.
func (s *AnalyticsService) CostAnalytics(since time.Time) (*CostStats, error) {
	sinceStr := since.Format("2006-01-02T15:04:05")
	stats := &CostStats{}

	// System-wide totals (completed uploads with actual_cost, excluding zero-cost)
	s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(CAST(actual_cost AS INTEGER)), 0), COALESCE(SUM(file_size), 0)
		 FROM uploads
		 WHERE status = 'completed' AND actual_cost IS NOT NULL AND actual_cost != '0' AND created_at >= ?`,
		sinceStr,
	).Scan(&stats.TotalUploads, &stats.TotalCost, &stats.TotalBytes)

	if stats.TotalUploads > 0 {
		totalCostInt := int64(0)
		_, _ = fmt.Sscanf(stats.TotalCost, "%d", &totalCostInt)
		stats.AvgCostPerUpload = fmt.Sprintf("%d", totalCostInt/stats.TotalUploads)
	} else {
		stats.AvgCostPerUpload = "0"
	}

	// By department (via token's department field)
	deptRows, err := s.db.Query(
		`SELECT COALESCE(t.department, 'unassigned'), COUNT(u.id), COALESCE(SUM(CAST(u.actual_cost AS INTEGER)), 0), COALESCE(SUM(u.file_size), 0)
		 FROM uploads u
		 LEFT JOIN api_tokens t ON u.token_id = t.id
		 WHERE u.status = 'completed' AND u.actual_cost IS NOT NULL AND u.actual_cost != '0' AND u.created_at >= ?
		 GROUP BY COALESCE(t.department, 'unassigned')
		 ORDER BY SUM(CAST(u.actual_cost AS INTEGER)) DESC`, sinceStr,
	)
	if err != nil {
		return nil, err
	}
	defer deptRows.Close()

	for deptRows.Next() {
		var dc DepartmentCost
		var totalCost int64
		if err := deptRows.Scan(&dc.Department, &dc.UploadCount, &totalCost, &dc.TotalBytes); err != nil {
			return nil, err
		}
		dc.TotalCost = fmt.Sprintf("%d", totalCost)
		stats.ByDepartment = append(stats.ByDepartment, dc)
	}

	// By token
	tokenRows, err := s.db.Query(
		`SELECT COALESCE(t.uuid, ''), COALESCE(t.name, 'direct'), COUNT(u.id), COALESCE(SUM(CAST(u.actual_cost AS INTEGER)), 0), COALESCE(SUM(u.file_size), 0)
		 FROM uploads u
		 LEFT JOIN api_tokens t ON u.token_id = t.id
		 WHERE u.status = 'completed' AND u.actual_cost IS NOT NULL AND u.actual_cost != '0' AND u.created_at >= ?
		 GROUP BY u.token_id
		 ORDER BY SUM(CAST(u.actual_cost AS INTEGER)) DESC LIMIT 20`, sinceStr,
	)
	if err != nil {
		return nil, err
	}
	defer tokenRows.Close()

	for tokenRows.Next() {
		var tc TokenCost
		var totalCost int64
		if err := tokenRows.Scan(&tc.TokenUUID, &tc.TokenName, &tc.UploadCount, &totalCost, &tc.TotalBytes); err != nil {
			return nil, err
		}
		tc.TotalCost = fmt.Sprintf("%d", totalCost)
		stats.ByToken = append(stats.ByToken, tc)
	}

	return stats, tokenRows.Err()
}
