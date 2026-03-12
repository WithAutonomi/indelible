package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/maidsafe/indelible/internal/config"
)

type contextKey string

const (
	UserIDKey     contextKey = "user_id"
	TokenIDKey    contextKey = "token_id"
	AuthMethodKey contextKey = "auth_method" // "jwt" or "api_token"
)

// Authenticate validates JWT session tokens or API bearer tokens.
func Authenticate(db *sql.DB, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			// TODO: Try JWT first, then API token lookup
			// For now, placeholder that passes through
			ctx := context.WithValue(r.Context(), UserIDKey, int64(0))
			ctx = context.WithValue(ctx, AuthMethodKey, "jwt")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin checks that the authenticated user has admin permissions.
func RequireAdmin(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO: Check effective permissions for user from context
			next.ServeHTTP(w, r)
		})
	}
}

// GetUserID extracts the authenticated user ID from the request context.
func GetUserID(ctx context.Context) int64 {
	id, _ := ctx.Value(UserIDKey).(int64)
	return id
}

// GetTokenID extracts the API token ID from the request context (0 if JWT auth).
func GetTokenID(ctx context.Context) int64 {
	id, _ := ctx.Value(TokenIDKey).(int64)
	return id
}
