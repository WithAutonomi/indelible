package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

const (
	ScimTokenIDKey contextKey = "scim_token_id"
)

// SCIMAuth validates SCIM bearer tokens and checks that SCIM is enabled.
func SCIMAuth(db *database.DB) func(http.Handler) http.Handler {
	settingSvc := services.NewSettingsService(db)
	tokenSvc := services.NewScimTokenService(db)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if SCIM is enabled
			enabled, err := settingSvc.Get("scim_enabled")
			if err != nil || enabled != "true" {
				w.Header().Set("Content-Type", "application/scim+json")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"SCIM is not enabled","status":404}`))
				return
			}

			// Extract Bearer token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("Content-Type", "application/scim+json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"missing authorization header","status":401}`))
				return
			}

			// V2-303: accept both "Bearer scim_<hex>" and bare "scim_<hex>".
			// Okta's "SCIM 2.0 Test App (Header Auth)" variant pastes the
			// token verbatim with no Bearer scheme, and the scim_ prefix is
			// already a strong token-type discriminator on its own.
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			if !strings.HasPrefix(tokenStr, "scim_") {
				w.Header().Set("Content-Type", "application/scim+json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"invalid authorization format","status":401}`))
				return
			}

			// Validate SCIM token
			scimToken, err := tokenSvc.Validate(tokenStr)
			if err != nil {
				w.Header().Set("Content-Type", "application/scim+json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"invalid or revoked SCIM token","status":401}`))
				return
			}

			// Record usage
			tokenSvc.RecordUsage(scimToken.ID)

			// Set context values for audit logging
			ctx := context.WithValue(r.Context(), ScimTokenIDKey, scimToken.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetScimTokenID extracts the SCIM token ID from the request context.
func GetScimTokenID(ctx context.Context) int64 {
	id, _ := ctx.Value(ScimTokenIDKey).(int64)
	return id
}
