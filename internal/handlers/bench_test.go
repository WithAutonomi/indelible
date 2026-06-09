package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupBenchRouter(b *testing.B) (http.Handler, string) {
	b.Helper()
	router := setupTestRouter(&testing.T{})

	// Register a user (neutral 202, no auto-login), then log in for a token.
	body, _ := json.Marshal(map[string]string{
		"email": "bench@test.com", "password": "password123",
		"first_name": "Bench", "last_name": "User",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	loginBody, _ := json.Marshal(map[string]string{
		"email": "bench@test.com", "password": "password123",
	})
	req = httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	token := resp["token"].(string)

	return router, token
}

func BenchmarkHealthCheck(b *testing.B) {
	router, _ := setupBenchRouter(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListUploads(b *testing.B) {
	router, token := setupBenchRouter(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/v2/uploads", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkAuthLogin(b *testing.B) {
	router, _ := setupBenchRouter(b)
	loginBody, _ := json.Marshal(map[string]string{
		"email": "bench@test.com", "password": "password123",
	})
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/v2/auth/login", bytes.NewReader(loginBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkTagSearch(b *testing.B) {
	router, token := setupBenchRouter(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/v2/tags/search?tag.env=production", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
