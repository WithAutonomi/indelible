package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// setSessionCookies sets the HttpOnly session cookie carrying the JWT and a
// non-HttpOnly csrf_token cookie carrying a random per-session token. The
// SPA reads csrf_token and echoes it back as X-CSRF-Token on mutations,
// where the CSRF middleware compares the two halves of the double-submit
// pair. Returns the generated CSRF token (callers may include it in the
// response body for clients that aren't browsers and so can't read cookies).
func setSessionCookies(w http.ResponseWriter, r *http.Request, jwtToken string, expiryHours int) (string, error) {
	csrfToken, err := newCSRFToken()
	if err != nil {
		return "", err
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	maxAge := expiryHours * 3600
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    jwtToken,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.CSRFCookieName,
		Value:    csrfToken,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: false, // SPA must read this to echo on mutations
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return csrfToken, nil
}

// clearSessionCookies expires both cookies set by setSessionCookies.
func clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	for _, name := range []string{"session", middleware.CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == "session",
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func newCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// --- Request/Response types ---

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type authResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

type userResponse struct {
	ID               int64  `json:"id"`
	Email            string `json:"email"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	IsActive         bool   `json:"is_active"`
	IsServiceAccount bool   `json:"is_service_account"`
	EmailVerified    bool   `json:"email_verified"`
	Permissions      string `json:"permissions"`
	CreatedAt        string `json:"created_at"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type updateProfileRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func toUserResponse(u *services.User, perms string) userResponse {
	return userResponse{
		ID:               u.ID,
		Email:            u.Email,
		FirstName:        u.FirstName,
		LastName:         u.LastName,
		IsActive:         u.IsActive,
		IsServiceAccount: u.IsServiceAccount,
		EmailVerified:    u.EmailVerified,
		Permissions:      perms,
		CreatedAt:        u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// --- Handlers ---

// Login godoc
// @Summary Log in with email and password
// @Description Authenticate a user with email and password credentials and return a JWT token
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body loginRequest true "Login credentials"
// @Success 200 {object} authResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /auth/login [post]
func Login(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)
	settingsSvc := services.NewSettingsService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" || req.Password == "" {
			jsonErrorWithCode(w, "email and password are required", "validation_error", http.StatusBadRequest)
			return
		}

		user, err := userSvc.GetByEmail(req.Email)
		if err != nil {
			// Constant-time: run a bcrypt compare even though the email is unknown,
			// so response time doesn't reveal whether the account exists (the
			// known-email path always runs CheckPassword below). Don't reveal
			// existence in the response, but audit the attempt with the supplied
			// email so credential-stuffing detection has something to work with.
			auth.DummyCheckPassword(req.Password)
			auditEvent(r, logSvc, "login_failed", "warn", nil, "unknown email: "+req.Email)
			jsonErrorWithCode(w, "invalid email or password", "invalid_credentials", http.StatusUnauthorized)
			return
		}

		if !user.IsActive {
			auditEvent(r, logSvc, "login_failed", "warn", &user.ID, "account inactive")
			jsonErrorWithCode(w, "account is inactive", "account_inactive", http.StatusForbidden)
			return
		}

		if !user.PasswordHash.Valid || !auth.CheckPassword(req.Password, user.PasswordHash.String) {
			auditEvent(r, logSvc, "login_failed", "warn", &user.ID, "incorrect password")
			jsonErrorWithCode(w, "invalid email or password", "invalid_credentials", http.StatusUnauthorized)
			return
		}

		// Get JWT expiry from settings (default 24h)
		expiryHours := 24
		if v, err := settingsSvc.Get("jwt_expiry_hours"); err == nil {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				expiryHours = n
			}
		}

		token, err := auth.GenerateToken(cfg.JWTKeyring().Primary(), user.ID, user.Email, expiryHours)
		if err != nil {
			jsonError(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		_ = userSvc.UpdateLastLogin(user.ID)
		auditEvent(r, logSvc, "login", "info", &user.ID, "password")

		if _, err := setSessionCookies(w, r, token, expiryHours); err != nil {
			jsonError(w, "failed to set session cookies", http.StatusInternalServerError)
			return
		}

		perms, _ := permSvc.GetEffective(user.ID)

		jsonResponse(w, http.StatusOK, authResponse{
			Token: token,
			User:  toUserResponse(user, perms),
		})
	}
}

// registrationEnabled reports whether self-registration is turned on. It is
// OFF unless an admin has explicitly set registration_enabled="true"; a missing
// or unparseable setting means disabled (fail-closed).
func registrationEnabled(s *services.SettingsService) bool {
	v, err := s.Get("registration_enabled")
	if err != nil {
		return false
	}
	return v == "true"
}

// Register godoc
// @Summary Register a new user
// @Description Create a new user account with email, password, and name. Self-registration is disabled by default; an admin must enable it via the registration_enabled setting. Self-registered users receive read-only access. To avoid account enumeration the endpoint always returns a neutral 202 (it never reveals whether the email already exists) and does not log the caller in — the user signs in afterward.
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body registerRequest true "Registration details"
// @Success 202 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /auth/register [post]
func Register(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)
	settingsSvc := services.NewSettingsService(db)
	verifySvc := services.NewEmailVerificationService(db)
	notifier := services.NewNotifier(cfg, db)

	return func(w http.ResponseWriter, r *http.Request) {
		// Self-registration is gated and disabled by default; an admin turns it
		// on via the registration_enabled setting. When off, 403 — this is the
		// primary fix for open/ungated registration. The bootstrap admin is
		// created out-of-band by SeedAdmin, never through this endpoint.
		if !registrationEnabled(settingsSvc) {
			jsonErrorWithCode(w, "self-registration is disabled", "registration_disabled", http.StatusForbidden)
			return
		}

		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		req.FirstName = strings.TrimSpace(req.FirstName)
		req.LastName = strings.TrimSpace(req.LastName)

		if req.Email == "" || req.Password == "" || req.FirstName == "" || req.LastName == "" {
			jsonError(w, "email, password, first_name, and last_name are required", http.StatusBadRequest)
			return
		}
		if !services.IsValidEmail(req.Email) {
			jsonError(w, "email is not a valid address", http.StatusBadRequest)
			return
		}
		if len(req.Password) < 8 {
			jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		// Anti-enumeration (V2-430): registration never reveals whether the email
		// already exists, and never auto-logs-in — an auto-login (token + session)
		// response would be trivially distinguishable from the email-taken case,
		// reintroducing the oracle. Both the freshly-created and already-registered
		// paths return the same neutral 202; the user proceeds by signing in (and
		// verifying their email). bcrypt runs on both paths so timing matches too.
		user, err := userSvc.Create(req.Email, hash, req.FirstName, req.LastName)
		if err != nil && !errors.Is(err, services.ErrEmailTaken) {
			jsonError(w, "failed to create user", http.StatusInternalServerError)
			return
		}
		if err == nil {
			// New account. Self-registered users always get read-only access; admin
			// is granted only via the bootstrap seed (SeedAdmin) or by an existing
			// admin — never by self-registration, which would be a first-registrant
			// land-grab on an exposed instance.
			_ = permSvc.SetDirect(user.ID, "read", user.ID)

			// Send verification email (best-effort — don't block registration).
			if token, err := verifySvc.Create(user.ID); err == nil {
				baseURL := cfg.BaseURL
				if baseURL == "" {
					baseURL = "http://localhost:8080"
				}
				_ = notifier.SendEmailVerification(user.Email, baseURL+"/verify-email?token="+token)
			}
		}

		jsonResponse(w, http.StatusAccepted, map[string]string{
			"message": "if this address can be registered, you'll receive a verification email",
		})
	}
}

// Logout clears the session cookie.
func Logout(db *database.DB) http.HandlerFunc {
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		// Logout is bound to a route under the Authenticate middleware (see
		// router.go), so the user ID is always set; nil-check defensively.
		userID := middleware.GetUserID(r.Context())
		var uidPtr *int64
		if userID != 0 {
			uidPtr = &userID
		}
		auditEvent(r, logSvc, "logout", "info", uidPtr, "")

		clearSessionCookies(w, r)
		jsonResponse(w, http.StatusOK, map[string]string{"status": "logged out"})
	}
}

// GetProfile godoc
// @Summary Get current user profile
// @Description Return the authenticated user's profile information
// @Tags Profile
// @Produce json
// @Success 200 {object} userResponse
// @Failure 404 {object} map[string]string
// @Router /me [get]
// @Security BearerAuth
func GetProfile(db *database.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		user, err := userSvc.GetByID(userID)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}

		perms, _ := permSvc.GetEffective(userID)
		jsonResponse(w, http.StatusOK, toUserResponse(user, perms))
	}
}

// UpdateProfile godoc
// @Summary Update current user profile
// @Description Update the authenticated user's first name and last name
// @Tags Profile
// @Accept json
// @Produce json
// @Param body body updateProfileRequest true "Profile fields to update"
// @Success 200 {object} userResponse
// @Failure 400 {object} map[string]string
// @Router /me [put]
// @Security BearerAuth
func UpdateProfile(db *database.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var req updateProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := userSvc.Update(userID, strings.TrimSpace(req.FirstName), strings.TrimSpace(req.LastName), nil); err != nil {
			jsonError(w, "failed to update profile", http.StatusInternalServerError)
			return
		}

		user, _ := userSvc.GetByID(userID)
		perms, _ := permSvc.GetEffective(userID)
		jsonResponse(w, http.StatusOK, toUserResponse(user, perms))
	}
}

// ChangePassword godoc
// @Summary Change password
// @Description Change the authenticated user's password by providing current and new password
// @Tags Profile
// @Accept json
// @Produce json
// @Param body body changePasswordRequest true "Current and new password"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /me/password [put]
// @Security BearerAuth
func ChangePassword(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var req changePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if len(req.NewPassword) < 8 {
			jsonError(w, "new password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		user, err := userSvc.GetByID(userID)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}

		if !user.PasswordHash.Valid || !auth.CheckPassword(req.CurrentPassword, user.PasswordHash.String) {
			jsonError(w, "current password is incorrect", http.StatusUnauthorized)
			return
		}

		hash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			jsonError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		if err := userSvc.UpdatePassword(userID, hash); err != nil {
			jsonError(w, "failed to update password", http.StatusInternalServerError)
			return
		}

		// password_changed_at is set by UpdatePassword — JWTs issued before
		// this timestamp are rejected by the auth middleware.
		auditEvent(r, logSvc, "password_changed", "info", &userID, "")

		jsonResponse(w, http.StatusOK, map[string]string{"message": "password updated"})
	}
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ForgotPassword godoc
// @Summary Request password reset email
// @Description Send a password reset link to the provided email address if it exists
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body forgotPasswordRequest true "Email address"
// @Success 200 {object} map[string]string
// @Router /auth/forgot-password [post]
func ForgotPassword(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	resetSvc := services.NewResetTokenService(db)
	logSvc := services.NewLogService(db)
	notifier := services.NewNotifier(cfg, db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req forgotPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))

		// Constant-time: always return success regardless of whether email exists
		// This prevents email enumeration attacks.
		defer func() {
			jsonResponse(w, http.StatusOK, map[string]string{
				"message": "if that email exists, a reset link has been sent",
			})
		}()

		user, err := userSvc.GetByEmail(req.Email)
		if err != nil {
			// Audit so a credential-stuffing burst at /forgot-password is
			// visible even though we don't reveal it to the caller.
			auditEvent(r, logSvc, "password_reset_requested", "warn", nil, "unknown email: "+req.Email)
			return
		}
		if !user.IsActive || user.IsServiceAccount {
			auditEvent(r, logSvc, "password_reset_requested", "warn", &user.ID, "inactive or service account")
			return
		}

		token, err := resetSvc.Create(user.ID)
		if err != nil {
			return // token creation failed — log but don't reveal
		}

		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:8080"
		}
		resetURL := baseURL + "/reset-password?token=" + token

		_ = notifier.SendPasswordReset(user.Email, resetURL)
		auditEvent(r, logSvc, "password_reset_requested", "info", &user.ID, "")
	}
}

// ResetPassword godoc
// @Summary Reset password with token
// @Description Reset a user's password using a valid reset token from the forgot-password email
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body resetPasswordRequest true "Reset token and new password"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /auth/reset-password [post]
func ResetPassword(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	resetSvc := services.NewResetTokenService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req resetPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if len(req.NewPassword) < 8 {
			jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		userID, err := resetSvc.Validate(req.Token)
		if err != nil {
			auditEvent(r, logSvc, "password_reset_completed", "warn", nil, "invalid or expired token")
			jsonError(w, "invalid or expired reset token", http.StatusUnauthorized)
			return
		}

		hash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			jsonError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		if err := userSvc.UpdatePassword(userID, hash); err != nil {
			jsonError(w, "failed to update password", http.StatusInternalServerError)
			return
		}

		// password_changed_at is set by UpdatePassword — JWTs issued before
		// this timestamp are rejected by the auth middleware.
		auditEvent(r, logSvc, "password_reset_completed", "info", &userID, "")

		jsonResponse(w, http.StatusOK, map[string]string{"message": "password reset successfully"})
	}
}

// VerifyEmail godoc
// @Summary Verify email address
// @Description Validate an email verification token and mark the user's email as verified
// @Tags Auth
// @Produce json
// @Param token query string true "Email verification token"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /auth/verify-email [get]
func VerifyEmail(db *database.DB) http.HandlerFunc {
	verifySvc := services.NewEmailVerificationService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			jsonError(w, "token is required", http.StatusBadRequest)
			return
		}

		userID, err := verifySvc.Validate(token)
		if err != nil {
			auditEvent(r, logSvc, "email_verified", "warn", nil, "invalid or expired token")
			jsonError(w, "invalid or expired verification token", http.StatusUnauthorized)
			return
		}
		auditEvent(r, logSvc, "email_verified", "info", &userID, "")

		jsonResponse(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
	}
}

// ResendVerification godoc
// @Summary Resend verification email
// @Description Generate a new email verification token and send it to the authenticated user
// @Tags Profile
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /me/resend-verification [post]
// @Security BearerAuth
func ResendVerification(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	verifySvc := services.NewEmailVerificationService(db)
	logSvc := services.NewLogService(db)
	notifier := services.NewNotifier(cfg, db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		user, err := userSvc.GetByID(userID)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}

		if user.EmailVerified {
			jsonResponse(w, http.StatusOK, map[string]string{"message": "email already verified"})
			return
		}

		token, err := verifySvc.Create(user.ID)
		if err != nil {
			jsonError(w, "failed to generate verification token", http.StatusInternalServerError)
			return
		}

		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:8080"
		}
		_ = notifier.SendEmailVerification(user.Email, baseURL+"/verify-email?token="+token)
		auditEvent(r, logSvc, "email_verification_requested", "info", &userID, "")

		jsonResponse(w, http.StatusOK, map[string]string{"message": "verification email sent"})
	}
}
