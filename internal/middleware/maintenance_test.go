package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
)

// seedUser inserts a user and, when level is non-empty, grants that direct
// permission level. Returns the new user's id. Avoids LastInsertId (unsupported
// by lib/pq) so it works on both SQLite and Postgres.
func seedUser(t *testing.T, db *database.DB, email, level string) int64 {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users (email) VALUES (?)`, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatalf("lookup user id: %v", err)
	}
	if level != "" {
		if _, err := db.Exec(
			`INSERT INTO user_permissions (user_id, permission_level) VALUES (?, ?)`,
			id, level,
		); err != nil {
			t.Fatalf("grant %s: %v", level, err)
		}
	}
	return id
}

// reqAs builds a request carrying an authenticated user id on its context,
// mimicking what Authenticate sets upstream of MaintenanceMode.
func reqAs(method, target string, userID int64) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(context.WithValue(req.Context(), UserIDKey, userID))
}

func setupMaintenanceDB(t *testing.T) *database.DB {
	return dbtest.OpenDB(t)
}

func setMaintenanceMode(t *testing.T, db *database.DB, enabled bool, message string) {
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

func TestMaintenanceMode_Enabled_AdminPassesThrough(t *testing.T) {
	db := setupMaintenanceDB(t)
	setMaintenanceMode(t, db, true, "")
	adminID := seedUser(t, db, "admin@example.com", "admin")

	called := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, reqAs("PATCH", "/api/v2/admin/settings", adminID))

	if w.Code != http.StatusOK {
		t.Fatalf("admin got %d during maintenance, want 200 (must reach the off-switch)", w.Code)
	}
	if !called {
		t.Error("downstream should be called for an admin during maintenance")
	}
}

func TestMaintenanceMode_Enabled_NonAdminBlocked(t *testing.T) {
	db := setupMaintenanceDB(t)
	setMaintenanceMode(t, db, true, "")
	userID := seedUser(t, db, "user@example.com", "write")

	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream should not be called for a non-admin during maintenance")
	})

	mw := MaintenanceMode(db)
	handler := mw(downstream)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, reqAs("GET", "/api/v2/uploads", userID))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-admin got %d during maintenance, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "maintenance_mode") {
		t.Errorf("body should contain maintenance_mode code: %s", w.Body.String())
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
