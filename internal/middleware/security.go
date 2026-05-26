package middleware

import "net/http"

// SecurityHeaders adds standard HTTP security headers to all responses.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data:; font-src 'self'; connect-src 'self'; "+
					"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			next.ServeHTTP(w, r)
		})
	}
}

// SwaggerCSP overrides the global CSP to allow Swagger UI's inline bootstrap
// script. Apply only on the /api/docs route — the strict default still
// covers the SPA + APIs. frame-ancestors stays 'none' so the docs page
// can't be embedded by a hostile site.
func SwaggerCSP() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self' 'unsafe-inline'; "+
					"style-src 'self' 'unsafe-inline'; img-src 'self' data:; "+
					"font-src 'self'; connect-src 'self'; "+
					"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			next.ServeHTTP(w, r)
		})
	}
}
