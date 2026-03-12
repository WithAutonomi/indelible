package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/maidsafe/indelible/internal/auth"
	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/middleware"
	"github.com/maidsafe/indelible/internal/services"
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

func Login(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" || req.Password == "" {
			jsonError(w, "email and password are required", http.StatusBadRequest)
			return
		}

		user, err := userSvc.GetByEmail(req.Email)
		if err != nil {
			// Constant-time: don't reveal whether email exists
			jsonError(w, "invalid email or password", http.StatusUnauthorized)
			return
		}

		if !user.IsActive {
			jsonError(w, "account is inactive", http.StatusForbidden)
			return
		}

		if !user.PasswordHash.Valid || !auth.CheckPassword(req.Password, user.PasswordHash.String) {
			jsonError(w, "invalid email or password", http.StatusUnauthorized)
			return
		}

		// Get JWT expiry from settings (default 24h)
		expiryHours := 24 // TODO: read from settings table

		token, err := auth.GenerateToken(cfg.JWTSecret, user.ID, user.Email, expiryHours)
		if err != nil {
			jsonError(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		_ = userSvc.UpdateLastLogin(user.ID)

		perms, _ := permSvc.GetEffective(user.ID)

		jsonResponse(w, http.StatusOK, authResponse{
			Token: token,
			User:  toUserResponse(user, perms),
		})
	}
}

func Register(db *sql.DB, cfg *config.Config) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

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
				jsonError(w, "email already registered", http.StatusConflict)
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

		expiryHours := 24 // TODO: read from settings table

		token, err := auth.GenerateToken(cfg.JWTSecret, user.ID, user.Email, expiryHours)
		if err != nil {
			jsonError(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, authResponse{
			Token: token,
			User:  toUserResponse(user, permLevel),
		})
	}
}

func GetProfile(db *sql.DB) http.HandlerFunc {
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

func UpdateProfile(db *sql.DB) http.HandlerFunc {
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

func ChangePassword(db *sql.DB, cfg *config.Config) http.HandlerFunc {
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

		// TODO: revoke all other sessions

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

func ForgotPassword(db *sql.DB, cfg *config.Config) http.HandlerFunc {
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
			return // email not found — respond identically
		}
		if !user.IsActive || user.IsServiceAccount {
			return // inactive or service account — no reset
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
	}
}

func ResetPassword(db *sql.DB, cfg *config.Config) http.HandlerFunc {
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

		// TODO: revoke all existing sessions for this user

		jsonResponse(w, http.StatusOK, map[string]string{"message": "password reset successfully"})
	}
}
