package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/database"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// withUserID injects a user ID into the request context so the idempotency
// middleware can extract it via GetUserID.
func withUserID(r *http.Request, userID int64) *http.Request {
	ctx := context.WithValue(r.Context(), UserIDKey, userID)
	return r.WithContext(ctx)
}

func TestIdempotency_POST_WithKey_CachesResponse(t *testing.T) {
	db := setupTestDB(t)

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":42}`))
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	// First request -- should hit downstream
	req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req.Header.Set("Idempotency-Key", "key-abc")
	req = withUserID(req, 1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first request: got %d, want 201", w.Code)
	}
	if w.Body.String() != `{"id":42}` {
		t.Errorf("first body = %q, want {\"id\":42}", w.Body.String())
	}
	if w.Header().Get("X-Idempotent-Replayed") != "" {
		t.Error("first request should not have X-Idempotent-Replayed header")
	}
}

func TestIdempotency_POST_ReplayReturnsCached(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":42}`))
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	// First request
	req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req.Header.Set("Idempotency-Key", "key-replay")
	req = withUserID(req, 1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Second request with same key
	req2 := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req2.Header.Set("Idempotency-Key", "key-replay")
	req2 = withUserID(req2, 1)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("replay: got %d, want 201", w2.Code)
	}
	if w2.Body.String() != `{"id":42}` {
		t.Errorf("replay body = %q, want {\"id\":42}", w2.Body.String())
	}
	if w2.Header().Get("X-Idempotent-Replayed") != "true" {
		t.Error("replay should have X-Idempotent-Replayed: true")
	}
	if callCount != 1 {
		t.Errorf("downstream called %d times, want 1", callCount)
	}
}

func TestIdempotency_GET_PassesThrough(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("list"))
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	// GET with idempotency key -- should still pass through every time
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v2/uploads", nil)
		req.Header.Set("Idempotency-Key", "key-get")
		req = withUserID(req, 1)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET %d: got %d, want 200", i+1, w.Code)
		}
	}

	if callCount != 3 {
		t.Errorf("downstream called %d times, want 3 (GET not cached)", callCount)
	}
}

func TestIdempotency_POST_WithoutKey_PassesThrough(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1}`))
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
		// No Idempotency-Key header
		req = withUserID(req, 1)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("POST %d: got %d, want 201", i+1, w.Code)
		}
	}

	if callCount != 3 {
		t.Errorf("downstream called %d times, want 3 (no key, no caching)", callCount)
	}
}

func TestIdempotency_DifferentUsers_SameKey_Independent(t *testing.T) {
	db := setupTestDB(t)

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := GetUserID(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if uid == 1 {
			w.Write([]byte(`{"owner":"alice"}`))
		} else {
			w.Write([]byte(`{"owner":"bob"}`))
		}
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	// User 1
	req1 := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req1.Header.Set("Idempotency-Key", "shared-key")
	req1 = withUserID(req1, 1)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	// User 2 with same key
	req2 := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req2.Header.Set("Idempotency-Key", "shared-key")
	req2 = withUserID(req2, 2)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w1.Body.String() != `{"owner":"alice"}` {
		t.Errorf("user 1 body = %q", w1.Body.String())
	}
	if w2.Body.String() != `{"owner":"bob"}` {
		t.Errorf("user 2 body = %q", w2.Body.String())
	}

	// Replay for user 1 should return alice's response
	req1r := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req1r.Header.Set("Idempotency-Key", "shared-key")
	req1r = withUserID(req1r, 1)
	w1r := httptest.NewRecorder()
	handler.ServeHTTP(w1r, req1r)

	if w1r.Body.String() != `{"owner":"alice"}` {
		t.Errorf("user 1 replay body = %q, want alice", w1r.Body.String())
	}
	if w1r.Header().Get("X-Idempotent-Replayed") != "true" {
		t.Error("user 1 replay should have X-Idempotent-Replayed")
	}
}

func TestIdempotency_ServerError_NotCached(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	// First request (500 should NOT be cached)
	req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req.Header.Set("Idempotency-Key", "key-500")
	req = withUserID(req, 1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Second request should hit downstream again
	req2 := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req2.Header.Set("Idempotency-Key", "key-500")
	req2 = withUserID(req2, 1)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if callCount != 2 {
		t.Errorf("downstream called %d times, want 2 (500 not cached)", callCount)
	}
}

func TestIdempotency_ClientError_IsCached(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad input"}`))
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req.Header.Set("Idempotency-Key", "key-400")
	req = withUserID(req, 1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Second request should return cached 400
	req2 := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	req2.Header.Set("Idempotency-Key", "key-400")
	req2 = withUserID(req2, 1)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if callCount != 1 {
		t.Errorf("downstream called %d times, want 1 (400 cached)", callCount)
	}
	if w2.Header().Get("X-Idempotent-Replayed") != "true" {
		t.Error("replay of 400 should have X-Idempotent-Replayed")
	}
}

func TestCleanupIdempotencyKeys(t *testing.T) {
	db := setupTestDB(t)

	// Insert an old key (> 24 hours ago)
	oldStamp := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := db.Exec(
		`INSERT INTO idempotency_keys (key, user_id, status_code, response_body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"old-key", 1, 201, `{"old":true}`, oldStamp,
	)
	if err != nil {
		t.Fatalf("insert old key: %v", err)
	}

	// Insert a fresh key
	_, err = db.Exec(
		`INSERT INTO idempotency_keys (key, user_id, status_code, response_body)
		 VALUES (?, ?, ?, ?)`,
		"fresh-key", 1, 201, `{"fresh":true}`,
	)
	if err != nil {
		t.Fatalf("insert fresh key: %v", err)
	}

	CleanupIdempotencyKeys(db)

	// Old key should be gone
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM idempotency_keys WHERE key = 'old-key'`).Scan(&count)
	if count != 0 {
		t.Error("old key should have been cleaned up")
	}

	// Fresh key should remain
	db.QueryRow(`SELECT COUNT(*) FROM idempotency_keys WHERE key = 'fresh-key'`).Scan(&count)
	if count != 1 {
		t.Error("fresh key should not be cleaned up")
	}
}

func TestIdempotencyCleanupInterval(t *testing.T) {
	interval := IdempotencyCleanupInterval()
	if interval != "3600s" {
		t.Errorf("interval = %q, want 3600s", interval)
	}
}

// Test the responseRecorder captures status and body correctly.
func TestResponseRecorder(t *testing.T) {
	underlying := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: underlying, statusCode: http.StatusOK}

	rec.WriteHeader(http.StatusAccepted)
	rec.Write([]byte("hello"))

	if rec.statusCode != http.StatusAccepted {
		t.Errorf("statusCode = %d, want 202", rec.statusCode)
	}
	if rec.body.String() != "hello" {
		t.Errorf("body = %q, want hello", rec.body.String())
	}
	// Underlying writer should also have the data
	if underlying.Code != http.StatusAccepted {
		t.Errorf("underlying code = %d, want 202", underlying.Code)
	}
}

// Verify PUT and DELETE are not cached
func TestIdempotency_PUT_PassesThrough(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})

	mw := Idempotency(db)
	handler := mw(downstream)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("PUT", "/api/v2/uploads/1", nil)
		req.Header.Set("Idempotency-Key", "key-put")
		req = withUserID(req, 1)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	if callCount != 2 {
		t.Errorf("downstream called %d times, want 2 (PUT not cached)", callCount)
	}
}

// Verify we are timing out on the insert delay to reduce test time
// via a small utility to confirm the query uses now() based on created_at.
func TestCleanupIdempotencyKeys_KeepsRecent(t *testing.T) {
	db := setupTestDB(t)

	// Insert key 23 hours ago -- should NOT be cleaned up
	recentStamp := time.Now().UTC().Add(-23 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := db.Exec(
		`INSERT INTO idempotency_keys (key, user_id, status_code, response_body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"recent-key", 1, 200, `{}`, recentStamp,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	CleanupIdempotencyKeys(db)

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM idempotency_keys WHERE key = 'recent-key'`).Scan(&count)
	if count != 1 {
		t.Error("recent key (23h old) should NOT have been cleaned up")
	}

	// Suppress unused import
	_ = time.Now()
}
