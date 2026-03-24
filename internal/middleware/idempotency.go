package middleware

import (
	"bytes"
	"database/sql"
	"net/http"
	"strconv"
)

// responseRecorder captures the status code and body written by downstream handlers.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Idempotency returns middleware that supports idempotent POST requests.
// When a request includes an Idempotency-Key header, the response is cached
// and replayed on subsequent requests with the same key.
func Idempotency(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only act on POST requests with an Idempotency-Key header
			if r.Method != "POST" {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			userID := GetUserID(r.Context())

			// Check for existing cached response
			var statusCode int
			var responseBody string
			err := db.QueryRow(
				`SELECT status_code, response_body FROM idempotency_keys WHERE key = ? AND user_id = ?`,
				key, userID,
			).Scan(&statusCode, &responseBody)

			if err == nil {
				// Replay cached response
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Idempotent-Replayed", "true")
				w.WriteHeader(statusCode)
				w.Write([]byte(responseBody))
				return
			}

			// Record the response
			rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Store the response for future replays (only for successful-ish responses)
			if rec.statusCode >= 200 && rec.statusCode < 500 {
				_, _ = db.Exec(
					`INSERT INTO idempotency_keys (key, user_id, status_code, response_body) VALUES (?, ?, ?, ?)`,
					key, userID, rec.statusCode, rec.body.String(),
				)
			}
		})
	}
}

// CleanupIdempotencyKeys removes expired idempotency keys (older than 24 hours).
func CleanupIdempotencyKeys(db *sql.DB) {
	_, _ = db.Exec(`DELETE FROM idempotency_keys WHERE created_at < datetime('now', '-1 day')`)
}

// IdempotencyCleanupInterval returns the recommended cleanup interval as a string.
func IdempotencyCleanupInterval() string {
	return strconv.Itoa(3600) + "s"
}
