package services

import (
	"testing"
	"time"
)

// insertTestUpload is a helper to insert uploads directly for analytics testing.
func insertTestUpload(t *testing.T, svc *UploadService, userID int64, filename string, fileSize int64, status string) *Upload {
	t.Helper()
	u, err := svc.Create(userID, nil, filename, filename, fileSize, "application/octet-stream", "public", "/tmp/"+filename, nil)
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	switch status {
	case "completed":
		svc.MarkCompleted(u.ID, "0xDATAMAP", "1000")
	case "failed":
		svc.MarkFailed(u.ID, "test error")
	}
	return u
}

func TestUploadAnalytics_Basic(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	analyticsSvc := NewAnalyticsService(db)

	user := createTestUser(t, userSvc, "test@example.com", "Test", "User")

	insertTestUpload(t, uploadSvc, user.ID, "file1.txt", 1024, "completed")
	insertTestUpload(t, uploadSvc, user.ID, "file2.txt", 2048, "completed")
	insertTestUpload(t, uploadSvc, user.ID, "file3.txt", 512, "failed")

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.UploadAnalytics(since)
	if err != nil {
		t.Fatalf("UploadAnalytics: %v", err)
	}
	if stats.TotalUploads != 3 {
		t.Errorf("TotalUploads = %d, want 3", stats.TotalUploads)
	}
	if stats.StatusCounts["completed"] != 2 {
		t.Errorf("completed count = %d, want 2", stats.StatusCounts["completed"])
	}
	if stats.StatusCounts["failed"] != 1 {
		t.Errorf("failed count = %d, want 1", stats.StatusCounts["failed"])
	}
	if stats.TotalBytes != 1024+2048+512 {
		t.Errorf("TotalBytes = %d", stats.TotalBytes)
	}
}

func TestUploadAnalytics_TopUploaders(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	analyticsSvc := NewAnalyticsService(db)

	u1 := createTestUser(t, userSvc, "user1@test.com", "User", "One")
	u2 := createTestUser(t, userSvc, "user2@test.com", "User", "Two")

	insertTestUpload(t, uploadSvc, u1.ID, "a.txt", 100, "completed")
	insertTestUpload(t, uploadSvc, u1.ID, "b.txt", 200, "completed")
	insertTestUpload(t, uploadSvc, u2.ID, "c.txt", 300, "completed")

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.UploadAnalytics(since)
	if err != nil {
		t.Fatalf("UploadAnalytics: %v", err)
	}
	if len(stats.TopUploaders) < 2 {
		t.Fatalf("expected at least 2 top uploaders, got %d", len(stats.TopUploaders))
	}
	// u1 should be first (2 uploads)
	if stats.TopUploaders[0].Count != 2 {
		t.Errorf("top uploader count = %d, want 2", stats.TopUploaders[0].Count)
	}
}

func TestUploadAnalytics_RecentFailures(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	analyticsSvc := NewAnalyticsService(db)

	user := createTestUser(t, userSvc, "test@example.com", "Test", "User")
	insertTestUpload(t, uploadSvc, user.ID, "fail.txt", 500, "failed")

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.UploadAnalytics(since)
	if err != nil {
		t.Fatalf("UploadAnalytics: %v", err)
	}
	if len(stats.RecentFailures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(stats.RecentFailures))
	}
	if stats.RecentFailures[0].Filename != "fail.txt" {
		t.Errorf("failure filename = %q", stats.RecentFailures[0].Filename)
	}
	if stats.RecentFailures[0].ErrorMessage != "test error" {
		t.Errorf("failure error = %q", stats.RecentFailures[0].ErrorMessage)
	}
}

func TestUploadAnalytics_AvgProcessingMs(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	analyticsSvc := NewAnalyticsService(db)

	user := createTestUser(t, userSvc, "test@example.com", "Test", "User")

	// Insert two completed uploads. We control processing_at and completed_at
	// directly so the average is deterministic regardless of wall clock.
	u1 := insertTestUpload(t, uploadSvc, user.ID, "a.bin", 100, "completed")
	u2 := insertTestUpload(t, uploadSvc, user.ID, "b.bin", 200, "completed")

	base := time.Now().UTC().Truncate(time.Second)
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

	// u1: 2000ms processing
	if _, err := db.Exec(
		`UPDATE uploads SET processing_at = ?, completed_at = ? WHERE id = ?`,
		stamp(base), stamp(base.Add(2*time.Second)), u1.ID,
	); err != nil {
		t.Fatalf("backdate u1: %v", err)
	}
	// u2: 4000ms processing
	if _, err := db.Exec(
		`UPDATE uploads SET processing_at = ?, completed_at = ? WHERE id = ?`,
		stamp(base), stamp(base.Add(4*time.Second)), u2.ID,
	); err != nil {
		t.Fatalf("backdate u2: %v", err)
	}

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.UploadAnalytics(since)
	if err != nil {
		t.Fatalf("UploadAnalytics: %v", err)
	}
	// Average of 2000ms and 4000ms is 3000ms.
	if stats.AvgProcessingMs != 3000 {
		t.Errorf("AvgProcessingMs = %d, want 3000", stats.AvgProcessingMs)
	}
}

func TestUploadAnalytics_Empty(t *testing.T) {
	db := setupTestDB(t)
	analyticsSvc := NewAnalyticsService(db)

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.UploadAnalytics(since)
	if err != nil {
		t.Fatalf("UploadAnalytics: %v", err)
	}
	if stats.TotalUploads != 0 {
		t.Errorf("expected 0 uploads, got %d", stats.TotalUploads)
	}
	if stats.AvgFileSize != 0 {
		t.Errorf("expected 0 avg file size, got %d", stats.AvgFileSize)
	}
}

func TestCostAnalytics_Basic(t *testing.T) {
	db := setupTestDB(t)
	userSvc := NewUserService(db)
	uploadSvc := NewUploadService(db)
	analyticsSvc := NewAnalyticsService(db)

	user := createTestUser(t, userSvc, "test@example.com", "Test", "User")

	// Create completed uploads with actual costs
	insertTestUpload(t, uploadSvc, user.ID, "file1.txt", 1024, "completed")
	insertTestUpload(t, uploadSvc, user.ID, "file2.txt", 2048, "completed")

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.CostAnalytics(since)
	if err != nil {
		t.Fatalf("CostAnalytics: %v", err)
	}

	// Both uploads have actual_cost="1000" set by insertTestUpload
	if stats.TotalUploads != 2 {
		t.Errorf("TotalUploads = %d, want 2", stats.TotalUploads)
	}
	if stats.TotalCost != "2000" {
		t.Errorf("TotalCost = %q, want '2000'", stats.TotalCost)
	}
	if stats.AvgCostPerUpload != "1000" {
		t.Errorf("AvgCostPerUpload = %q, want '1000'", stats.AvgCostPerUpload)
	}
}

func TestCostAnalytics_Empty(t *testing.T) {
	db := setupTestDB(t)
	analyticsSvc := NewAnalyticsService(db)

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.CostAnalytics(since)
	if err != nil {
		t.Fatalf("CostAnalytics: %v", err)
	}
	if stats.TotalUploads != 0 {
		t.Errorf("TotalUploads = %d", stats.TotalUploads)
	}
	if stats.AvgCostPerUpload != "0" {
		t.Errorf("AvgCostPerUpload = %q", stats.AvgCostPerUpload)
	}
}

func TestTokenAnalytics_Empty(t *testing.T) {
	db := setupTestDB(t)
	analyticsSvc := NewAnalyticsService(db)

	since := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := analyticsSvc.TokenAnalytics(since)
	if err != nil {
		t.Fatalf("TokenAnalytics: %v", err)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d", stats.TotalRequests)
	}
	if stats.ActiveTokens != 0 {
		t.Errorf("ActiveTokens = %d", stats.ActiveTokens)
	}
}
