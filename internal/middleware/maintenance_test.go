package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
)

func setupMaintenanceDB(t *testing.T) *sql.DB {
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

func setMaintenanceMode(t *testing.T, db *sql.DB, enabled bool, message string) {
	t.Helper()
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := db.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES ('maintenance_mode', ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = ?`,
		val, val,
	)
	if err != nil {
		t.Fatalf("set maintenance_mode: %v", err)
	}

	if message != "" {
		_, err = db.Exec(
			`INSERT INTO settings (key, value, updated_at) VALUES ('maintenance_message', ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET value = ?`,
			message, message,
		)
		if err != nil {
			t.Fatalf("set maintenance_message: %v", err)
		}
	}
}

func TestMaintenanceMode_Disabled_PassesThrough(t *testing.T) {
	db := setupMaintenanceDB(t)
	// No maintenance_mode setting at all -- default is off

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	req := httptest.NewRequest("GET", "/api/v2/uploads", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 when maintenance is off", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", w.Body.String())
	}
}

func TestMaintenanceMode_Enabled_Returns503(t *testing.T) {
	db := setupMaintenanceDB(t)
	setMaintenanceMode(t, db, true, "")

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream should not be called during maintenance")
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	req := httptest.NewRequest("GET", "/api/v2/uploads", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "maintenance_mode") {
		t.Errorf("body should contain maintenance_mode code: %s", body)
	}
	// Default message
	if !strings.Contains(body, "System is under maintenance") {
		t.Errorf("body should contain default message: %s", body)
	}
}

func TestMaintenanceMode_Enabled_CustomMessage(t *testing.T) {
	db := setupMaintenanceDB(t)
	setMaintenanceMode(t, db, true, "Upgrading to v2.0, back at 3pm UTC")

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream should not be called")
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	req := httptest.NewRequest("POST", "/api/v2/uploads", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Upgrading to v2.0") {
		t.Errorf("body should contain custom message: %s", w.Body.String())
	}
}

func TestMaintenanceMode_ExplicitlyDisabled_PassesThrough(t *testing.T) {
	db := setupMaintenanceDB(t)
	setMaintenanceMode(t, db, false, "")

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	req := httptest.NewRequest("GET", "/api/v2/uploads", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 when maintenance is explicitly false", w.Code)
	}
}

func TestMaintenanceMode_ToggleDynamic(t *testing.T) {
	db := setupMaintenanceDB(t)

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	// Initially off -- should pass through
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initially got %d, want 200", w.Code)
	}

	// Turn on maintenance
	setMaintenanceMode(t, db, true, "")

	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("after enabling: got %d, want 503", w2.Code)
	}

	// Turn off maintenance
	setMaintenanceMode(t, db, false, "")

	req3 := httptest.NewRequest("GET", "/test", nil)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("after disabling: got %d, want 200", w3.Code)
	}
}

func TestMaintenanceMode_AllHTTPMethods_Blocked(t *testing.T) {
	db := setupMaintenanceDB(t)
	setMaintenanceMode(t, db, true, "")

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream should not be called")
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/v2/something", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: got %d, want 503", method, w.Code)
		}
	}
}
