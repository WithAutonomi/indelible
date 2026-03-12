package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/maidsafe/indelible/internal/auth"
	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/services"
)

type contextKey string

const (
	UserIDKey     contextKey = "user_id"
	TokenIDKey    contextKey = "token_id"
	AuthMethodKey contextKey = "auth_method" // "jwt" or "api_token"
)

// Authenticate validates JWT session tokens or API bearer tokens.
func Authenticate(db *sql.DB, cfg *config.Config) func(http.Handler) http.Handler {
	userSvc := services.NewUserService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenStr == authHeader {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			// Try JWT first
			claims, err := auth.ValidateToken(cfg.JWTSecret, tokenStr)
			if err == nil {
				// Verify user still exists and is active
				user, err := userSvc.GetByID(claims.UserID)
				if err != nil || !user.IsActive {
					http.Error(w, `{"error":"user not found or inactive"}`, http.StatusUnauthorized)
					return
				}

				ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
				ctx = context.WithValue(ctx, AuthMethodKey, "jwt")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// TODO: Try API token lookup (bcrypt compare against token_hash)
			// This will be implemented in Phase 2 (API Tokens)

			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
		})
	}
}

// RequireAdmin checks that the authenticated user has admin permissions.
func RequireAdmin(db *sql.DB) func(http.Handler) http.Handler {
	permSvc := services.NewPermissionService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			isAdmin, err := permSvc.IsAdmin(userID)
			if err != nil || !isAdmin {
				http.Error(w, `{"error":"admin access required"}`, http.StatusForbidden)
				return
			}
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
