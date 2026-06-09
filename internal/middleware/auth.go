package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

type contextKey string

const (
	UserIDKey     contextKey = "user_id"
	TokenIDKey    contextKey = "token_id"
	AuthMethodKey contextKey = "auth_method" // "jwt" or "api_token"
	AuthSourceKey contextKey = "auth_source" // "cookie" or "header"
)

// AuthSource constants identify where the credential was carried.
// CSRF defence applies only to AuthSourceCookie — Bearer callers are not
// browsers and cannot be tricked into cross-origin POSTs.
const (
	AuthSourceCookie = "cookie"
	AuthSourceHeader = "header"
)

// Authenticate validates JWT session tokens or API bearer tokens.
func Authenticate(db *database.DB, cfg *config.Config) func(http.Handler) http.Handler {
	userSvc := services.NewUserService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token: try httpOnly cookie first, then Authorization header.
			// authSource is propagated on the context so CSRF middleware can
			// enforce double-submit-token only on cookie-authenticated mutations.
			var tokenStr, authSource string
			if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
				tokenStr = cookie.Value
				authSource = AuthSourceCookie
			} else {
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					http.Error(w, `{"error":"missing authorization","code":"unauthorized"}`, http.StatusUnauthorized)
					return
				}
				tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
				if tokenStr == authHeader {
					http.Error(w, `{"error":"invalid authorization format","code":"unauthorized"}`, http.StatusUnauthorized)
					return
				}
				authSource = AuthSourceHeader
			}

			// Try JWT first
			claims, err := auth.ValidateToken(cfg.JWTSecret, tokenStr, cfg.JWTSecretsPrevious...)
			if err == nil {
				// Verify user still exists and is active
				user, err := userSvc.GetByID(claims.UserID)
				if err != nil || !user.IsActive {
					http.Error(w, `{"error":"user not found or inactive","code":"unauthorized"}`, http.StatusUnauthorized)
					return
				}

				// Reject JWTs issued before a password change. Compare at
				// whole-second granularity: JWT iat is a Unix-second integer,
				// but PasswordChangedAt carries sub-second precision from the
				// DB. A re-login completing in the same wall-clock second as
				// the password change would otherwise be 401'd, because the
				// JWT's iat truncates to .000 while the column reads .NNN.
				if user.PasswordChangedAt.Valid && claims.IssuedAt != nil {
					if claims.IssuedAt.Time.Unix() < user.PasswordChangedAt.Time.Unix() {
						http.Error(w, `{"error":"session invalidated by password change","code":"unauthorized"}`, http.StatusUnauthorized)
						return
					}
				}

				ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
				ctx = context.WithValue(ctx, AuthMethodKey, "jwt")
				ctx = context.WithValue(ctx, AuthSourceKey, authSource)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Try API token
			tokenSvc := services.NewTokenService(db)
			apiToken, err := tokenSvc.ValidateSecret(tokenStr)
			if err == nil {
				// Verify owning user still exists and is active
				user, err := userSvc.GetByID(apiToken.UserID)
				if err != nil || !user.IsActive {
					http.Error(w, `{"error":"token owner not found or inactive","code":"unauthorized"}`, http.StatusUnauthorized)
					return
				}

				// Record usage
				tokenSvc.RecordUsage(apiToken.ID)

				ctx := context.WithValue(r.Context(), UserIDKey, apiToken.UserID)
				ctx = context.WithValue(ctx, TokenIDKey, apiToken.ID)
				ctx = context.WithValue(ctx, AuthMethodKey, "api_token")
				ctx = context.WithValue(ctx, AuthSourceKey, authSource)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			http.Error(w, `{"error":"invalid or expired token","code":"unauthorized"}`, http.StatusUnauthorized)
		})
	}
}

// RequireAdmin checks that the authenticated user has admin permissions.
func RequireAdmin(db *database.DB) func(http.Handler) http.Handler {
	permSvc := services.NewPermissionService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			isAdmin, err := permSvc.IsAdmin(userID)
			if err != nil || !isAdmin {
				http.Error(w, `{"error":"admin access required","code":"forbidden"}`, http.StatusForbidden)
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

// GetAuthSource returns the auth credential source (AuthSourceCookie or
// AuthSourceHeader). Empty if Authenticate has not run for this request.
func GetAuthSource(ctx context.Context) string {
	s, _ := ctx.Value(AuthSourceKey).(string)
	return s
}
