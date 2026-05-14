package handlers

import (
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
			// Constant-time: don't reveal whether email exists
			jsonErrorWithCode(w, "invalid email or password", "invalid_credentials", http.StatusUnauthorized)
			return
		}

		if !user.IsActive {
			jsonErrorWithCode(w, "account is inactive", "account_inactive", http.StatusForbidden)
			return
		}

		if !user.PasswordHash.Valid || !auth.CheckPassword(req.Password, user.PasswordHash.String) {
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

		token, err := auth.GenerateToken(cfg.JWTSecret, user.ID, user.Email, expiryHours)
		if err != nil {
			jsonError(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		_ = userSvc.UpdateLastLogin(user.ID)

		// Set httpOnly session cookie (browser auth)
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			MaxAge:   expiryHours * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			SameSite: http.SameSiteLaxMode,
		})

		perms, _ := permSvc.GetEffective(user.ID)

		jsonResponse(w, http.StatusOK, authResponse{
			Token: token,
			User:  toUserResponse(user, perms),
		})
	}
}

// Register godoc
// @Summary Register a new user
// @Description Create a new user account with email, password, and name
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body registerRequest true "Registration details"
// @Success 201 {object} authResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /auth/register [post]
func Register(db *database.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)
	settingsSvc := services.NewSettingsService(db)
	verifySvc := services.NewEmailVerificationService(db)
	notifier := services.NewNotifier(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
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
		if len(req.Password) < 8 {
			jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		user, err := userSvc.Create(req.Email, hash, req.FirstName, req.LastName)
		if err != nil {
			if errors.Is(err, services.ErrEmailTaken) {
				jsonErrorWithCode(w, "email already registered", "email_taken", http.StatusConflict)
				return
			}
			jsonError(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		// First user becomes admin
		count, _ := userSvc.Count()
		permLevel := "read" // default
		if count == 1 {
			permLevel = "admin"
		}
		_ = permSvc.SetDirect(user.ID, permLevel, user.ID)

		// Send verification email (best-effort â€” don't block registration)
		if token, err := verifySvc.Create(user.ID); err == nil {
			baseURL := cfg.BaseURL
			if baseURL == "" {
				baseURL = "http://localhost:8080"
			}
			_ = notifier.SendEmailVerification(user.Email, baseURL+"/verify-email?token="+token)
		}

		expiryHours := 24
		if v, err := settingsSvc.Get("jwt_expiry_hours"); err == nil {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				expiryHours = n
			}
		}

		token, err := auth.GenerateToken(cfg.JWTSecret, user.ID, user.Email, expiryHours)
		if err != nil {
			jsonError(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			MaxAge:   expiryHours * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			SameSite: http.SameSiteLaxMode,
		})

		jsonResponse(w, http.StatusCreated, authResponse{
			Token: token,
			User:  toUserResponse(user, permLevel),
		})
	}
}

// Logout clears the session cookie.
func Logout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			SameSite: http.SameSiteLaxMode,
		})
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

		// password_changed_at is set by UpdatePassword â€” JWTs issued before
		// this timestamp are rejected by the auth middleware.

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
	notifier := services.NewNotifier(cfg)

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
			return // email not found â€” respond identically
		}
		if !user.IsActive || user.IsServiceAccount {
			return // inactive or service account â€” no reset
		}

		token, err := resetSvc.Create(user.ID)
		if err != nil {
			return // token creation failed â€” log but don't reveal
		}

		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:8080"
		}
		resetURL := baseURL + "/reset-password?token=" + token

		_ = notifier.SendPasswordReset(user.Email, resetURL)
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

		// password_changed_at is set by UpdatePassword â€” JWTs issued before
		// this timestamp are rejected by the auth middleware.

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

	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			jsonError(w, "token is required", http.StatusBadRequest)
			return
		}

		_, err := verifySvc.Validate(token)
		if err != nil {
			jsonError(w, "invalid or expired verification token", http.StatusUnauthorized)
			return
		}

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
	notifier := services.NewNotifier(cfg)

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

		jsonResponse(w, http.StatusOK, map[string]string{"message": "verification email sent"})
	}
}
