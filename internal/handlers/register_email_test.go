package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRegister_RejectsMalformedEmail covers the email-format validation:
// /auth/register must reject a non-email string with 400 rather than creating a
// junk account.
func TestRegister_RejectsMalformedEmail(t *testing.T) {
	router := setupTestRouter(t) // registration enabled

	for _, bad := range []string{"notanemail", "no-at-sign.com", "foo <bar@baz.com>"} {
		body, _ := json.Marshal(map[string]string{
			"email":      bad,
			"password":   "password123",
			"first_name": "Test",
			"last_name":  "User",
		})
		req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("register %q: got %d, want 400; body: %s", bad, w.Code, w.Body.String())
		}
	}

	// A well-formed address still registers.
	body, _ := json.Marshal(map[string]string{
		"email":      "valid.user@example.com",
		"password":   "password123",
		"first_name": "Test",
		"last_name":  "User",
	})
	req := httptest.NewRequest("POST", "/api/v2/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register valid email: got %d, want 201; body: %s", w.Code, w.Body.String())
	}
}
