package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
)

// CSRFCookieName is the cookie carrying the double-submit token. Non-HttpOnly
// so the SPA can read it and echo it back as the X-CSRF-Token header. It is
// set at session-create time (Login / Register / OIDC callback) alongside
// the HttpOnly session cookie.
const CSRFCookieName = "csrf_token"

// CSRFHeaderName is the request header the SPA echoes the cookie value into.
const CSRFHeaderName = "X-CSRF-Token"

// CSRF implements double-submit-token CSRF protection for cookie-authenticated
// browser requests.
//
// Rules:
//   - Read methods (GET / HEAD / OPTIONS) are always allowed.
//   - Bearer / API-token callers are exempt — they are not browsers and
//     cannot be tricked into cross-origin POSTs.
//   - Cookie-authenticated mutations must carry an X-CSRF-Token header whose
//     value matches the csrf_token cookie set at session creation.
//
// When enforce is false (report-only mode), mismatches are logged but the
// request is allowed through. This lets a deployment ride one release cycle
// in observation mode before flipping to enforce.
//
// Threat model: an attacker on a different origin can make a request to
// indelible carrying the user's session cookie (SameSite=Lax mitigates
// most but not all such flows). The attacker cannot read the csrf_token
// cookie (same-origin policy) and so cannot set the X-CSRF-Token header
// to a matching value. An attacker with same-origin XSS can read both
// cookies and bypass CSRF — that's a different mitigation (CSP, output
// encoding) and CSRF is not meant to defend against it.
func CSRF(enforce bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			// Bearer / API-token: not a browser, not vulnerable, skip.
			if GetAuthSource(r.Context()) != AuthSourceCookie {
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get(CSRFHeaderName)
			cookie, cookieErr := r.Cookie(CSRFCookieName)

			mismatch := cookieErr != nil ||
				header == "" ||
				cookie.Value == "" ||
				subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1

			if mismatch {
				slog.Warn("csrf token mismatch",
					"method", r.Method,
					"path", r.URL.Path,
					"has_header", header != "",
					"has_cookie", cookieErr == nil && cookie != nil && cookie.Value != "",
					"enforce", enforce,
					"user_id", GetUserID(r.Context()),
				)
				if enforce {
					http.Error(w, `{"error":"csrf token invalid or missing","code":"forbidden"}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
