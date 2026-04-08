package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBodySize_UnderLimit(t *testing.T) {
	handler := MaxBodySize(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected error reading body: %v", err)
		}
		if len(body) != 10 {
			t.Errorf("body length = %d, want 10", len(body))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader("0123456789"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestMaxBodySize_OverLimit(t *testing.T) {
	handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err == nil {
			t.Fatal("expected error reading oversized body")
		}
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 100)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}

func TestMaxBodySize_SkipsMultipart(t *testing.T) {
	handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("multipart body should not be limited: %v", err)
		}
		if len(body) < 10 {
			t.Errorf("expected full multipart body, got %d bytes", len(body))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 100)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for multipart", w.Code)
	}
}
